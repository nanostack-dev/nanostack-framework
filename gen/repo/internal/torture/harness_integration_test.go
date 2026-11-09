//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// The isolation harness checks each derived method against an oracle: the
// rows that were seeded, filtered in Go by what the method's name says. Every
// tenant of the world has rows with the same values, so a method that drops a
// scope predicate returns another tenant's row and the comparison fails. Each
// delete runs in a transaction that is rolled back, and the table is read
// directly afterwards: the other tenants' rows must still be there.

// The world: indexes 0 and 1 are seeded, 2 is a tenant with no row.
//
//nolint:gochecknoglobals // fixture
var (
	orgs = [3]uuid.UUID{
		uuid.MustParse("a0000000-0000-4000-8000-000000000001"),
		uuid.MustParse("b0000000-0000-4000-8000-000000000002"),
		uuid.MustParse("c0000000-0000-4000-8000-000000000003"),
	}
	products = [3]uuid.UUID{
		uuid.MustParse("a1000000-0000-4000-8000-000000000001"),
		uuid.MustParse("b1000000-0000-4000-8000-000000000002"),
		uuid.MustParse("c1000000-0000-4000-8000-000000000003"),
	}
	sites = [3]uuid.UUID{
		uuid.MustParse("a2000000-0000-4000-8000-000000000001"),
		uuid.MustParse("b2000000-0000-4000-8000-000000000002"),
		uuid.MustParse("c2000000-0000-4000-8000-000000000003"),
	}
	// workspaces: "w1" and "w2" are seeded, "wX" is not.
	workspaces = []string{"w1", "w2"}
	// ctxValues are the workspaces a request context may carry; "" is none.
	ctxValues = []string{"", "w1", "w2", "wX"}
)

// idOf is the uuid of a row label.
func idOf(label string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(label)) }

// trow is a seeded row, as the oracle knows it.
type trow struct {
	label              string
	org, product, site int
	ws                 string
	wsNull             bool
	name, nameKey, by  string
	memo               string
	seq                int64
	count              int32
	flag, flag2        bool
	text, text2, text3 string
	deleted            bool
	external           string
	externalNull       bool
	handle, email      string
}

type scopeSet uint8

const (
	scOrg scopeSet = 1 << iota
	scProduct
	scSite
)

type wsMode int

const (
	wsNone  wsMode = iota // the name does not filter on workspace_id
	wsCtx                 // Workspace: workspacescope.Filter on ctx
	wsParam               // WorkspaceID: plain equality on a parameter
)

// args are the arguments of one call: the tenants by index, and the values.
type args struct {
	org, product, site int
	ws                 string
	s, s2              string
	n                  int64
	b                  bool
}

// spec is a derived method and what its name says it filters on.
type spec struct {
	name string
	sc   scopeSet
	ws   wsMode
	// vals are the values of the non-scope parameters to try; one empty set
	// when the method has none.
	vals []args
	pred func(r trow, a args) bool
	// exactly one of these is set. find returns the labels of the rows read.
	find   func(ctx context.Context, a args) ([]string, error)
	count  func(ctx context.Context, a args) (int64, error)
	exists func(ctx context.Context, a args) (bool, error)
	// del returns the rows deleted, -1 when the result shape does not say.
	del func(ctx context.Context, a args) (int64, error)
	// single: the method returns at most one row, and a second match is
	// ErrMoreThanOneRow. get: no row is sql.ErrNoRows.
	single, get bool
}

// world is a seeded table: its name, how its label reads back, its rows.
type world struct {
	table string
	label string // SQL expression of the label of a row
	rows  []trow
}

func (w world) remaining(t *testing.T, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) []string {
	t.Helper()
	rows, err := q.QueryContext(context.Background(), "SELECT ("+w.label+")::text FROM "+w.table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err = rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func (s *spec) matches(r trow, a args, ctxWS string) bool {
	if s.sc&scOrg != 0 && r.org != a.org || s.sc&scProduct != 0 && r.product != a.product ||
		s.sc&scSite != 0 && r.site != a.site {
		return false
	}
	switch s.ws {
	case wsCtx:
		if ctxWS != "" && (r.wsNull || r.ws != ctxWS) {
			return false
		}
	case wsParam:
		if r.wsNull || r.ws != a.ws {
			return false
		}
	case wsNone:
	}
	return s.pred == nil || s.pred(r, a)
}

func (s *spec) kind() string {
	switch {
	case s.find != nil:
		return "find"
	case s.count != nil:
		return "count"
	case s.exists != nil:
		return "exists"
	}
	return "delete"
}

// each calls f with every combination of the tenants the name filters on, the
// workspaces ctx may carry and the values.
func (s *spec) each(f func(a args, ctxWS string)) {
	idx := func(on bool) []int {
		if on {
			return []int{0, 1, 2}
		}
		return []int{0}
	}
	vals := s.vals
	if len(vals) == 0 {
		vals = []args{{}}
	}
	wss := []string{""}
	if s.ws == wsParam {
		wss = []string{"w1", "w2", "wX"}
	}
	for _, o := range idx(s.sc&scOrg != 0) {
		for _, p := range idx(s.sc&scProduct != 0) {
			for _, st := range idx(s.sc&scSite != 0) {
				for _, ws := range wss {
					for _, v := range vals {
						for _, c := range ctxValues {
							a := v
							a.org, a.product, a.site, a.ws = o, p, st, ws
							f(a, c)
						}
					}
				}
			}
		}
	}
}

func ctxOf(ws string) context.Context {
	if ws == "" {
		return context.Background()
	}
	return workspacescope.With(context.Background(), ws)
}

// runSpecs checks every spec of w against its oracle.
func runSpecs(t *testing.T, db *sql.DB, w world, specs []spec) {
	t.Helper()
	all := make([]string, len(w.rows))
	for i, r := range w.rows {
		all[i] = r.label
	}
	slices.Sort(all)
	if got := w.remaining(t, db); !slices.Equal(got, all) {
		t.Fatalf("seeded %v, read back %v", all, got)
	}
	for i := range specs {
		s := &specs[i]
		t.Run(s.name, func(t *testing.T) {
			calls, nonEmpty := 0, 0
			s.each(func(a args, ctxWS string) {
				calls++
				var want []string
				for _, r := range w.rows {
					if s.matches(r, a, ctxWS) {
						want = append(want, r.label)
					}
				}
				slices.Sort(want)
				if len(want) > 0 {
					nonEmpty++
				}
				ctx := ctxOf(ctxWS)
				where := fmt.Sprintf(
					"org=%d product=%d site=%d ws=%q ctx=%q %+v",
					a.org,
					a.product,
					a.site,
					a.ws,
					ctxWS,
					a,
				)
				switch s.kind() {
				case "find":
					checkFind(ctx, t, s, a, want, where)
				case "count":
					got, err := s.count(ctx, a)
					if err != nil || got != int64(len(want)) {
						t.Fatalf("%s: count %d, %v; want %d", where, got, err, len(want))
					}
				case "exists":
					got, err := s.exists(ctx, a)
					if err != nil || got != (len(want) > 0) {
						t.Fatalf("%s: exists %v, %v; want %v", where, got, err, len(want) > 0)
					}
				case "delete":
					checkDelete(ctx, t, db, w, s, a, want, where)
				}
			})
			if nonEmpty == 0 {
				t.Fatalf("no call of %d matches a row: the oracle proves nothing", calls)
			}
		})
	}
}

func checkFind(ctx context.Context, t *testing.T, s *spec, a args, want []string, where string) {
	t.Helper()
	got, err := s.find(ctx, a)
	slices.Sort(got)
	switch {
	case s.single && len(want) > 1:
		if !errors.Is(err, transactor.ErrMoreThanOneRow) {
			t.Fatalf("%s: got %v, %v; want ErrMoreThanOneRow for %v", where, got, err, want)
		}
	case s.get && len(want) == 0:
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s: got %v, %v; want sql.ErrNoRows", where, got, err)
		}
	case err != nil || !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0):
		t.Fatalf("%s: got %v, %v; want %v", where, got, err, want)
	}
}

func checkDelete(ctx context.Context, t *testing.T, db *sql.DB, w world, s *spec, a args, want []string, where string) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	n, err := s.del(transactor.WithTx(ctx, tx), a)
	if err != nil {
		t.Fatalf("%s: delete: %v", where, err)
	}
	if n >= 0 && n != int64(len(want)) {
		t.Fatalf("%s: deleted %d rows, want %d", where, n, len(want))
	}
	var keep []string
	for _, r := range w.rows {
		if !slices.Contains(want, r.label) {
			keep = append(keep, r.label)
		}
	}
	slices.Sort(keep)
	if got := w.remaining(t, tx); !slices.Equal(got, keep) {
		t.Fatalf(
			"%s: after the delete the table holds %d rows %v, want %d: it deleted the wrong rows (it should delete only %v)",
			where,
			len(got),
			got,
			len(keep),
			want,
		)
	}
}

// labs reads the label of each row a find returned.
func labs[T any](label func(T) string) func([]T, error) ([]string, error) {
	return func(rows []T, err error) ([]string, error) {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = label(r)
		}
		return out, err
	}
}

// opt reads the label of the row an Option find returned.
func opt[T any](label func(T) string) func(functional.Option[T], error) ([]string, error) {
	return func(o functional.Option[T], err error) ([]string, error) {
		if row, ok := o.Get(); ok {
			return []string{label(row)}, err
		}
		return nil, err
	}
}

// one reads the label of the row a GetBy returned.
func one[T any](label func(T) string) func(T, error) ([]string, error) {
	return func(row T, err error) ([]string, error) {
		if err != nil {
			return nil, err
		}
		return []string{label(row)}, nil
	}
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func affected(ok bool, err error) (int64, error) {
	if ok {
		return 1, err
	}
	return 0, err
}

func none(err error) (int64, error) { return -1, err }

func vals(strs ...string) []args {
	out := make([]args, len(strs))
	for i, s := range strs {
		out[i] = args{s: s}
	}
	return out
}
