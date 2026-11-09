//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// ktype is a column type of kitchen: the suffix of its fields (CText, NText),
// and the values of its two rows.
type ktype struct {
	name   string
	v1, v2 any
	// probes are other values, with the rows (1 or 2, 0 for none) they must match.
	probes []probe
}

type probe struct {
	v    any
	want int
	why  string
}

func tz(h int) *time.Location { return time.FixedZone("z", h*3600) }

func ktypes() []ktype {
	return []ktype{
		{name: "UUID", v1: idOf("u1"), v2: idOf("u2")},
		{
			name: "Text",
			v1:   "alpha",
			v2:   `optLbl'Brien "q" %_\`,
			probes: []probe{{"ALPHA", 0, "text equality is case sensitive"},
				{"alph", 0, "no prefix match"}, {"%", 0, "% is no wildcard"}, {"", 0, "empty string"}},
		},
		{name: "Varchar", v1: "v1", v2: "v2"},
		{
			name: "Citext",
			v1:   "Mixed",
			v2:   "other",
			probes: []probe{
				{
					"mixed",
					0,
					"a string predicate compares as text (documented on the where type), so a citext column is case sensitive here",
				},
			},
		},
		{name: "Small", v1: int16(1), v2: int16(32767)},
		{name: "Int", v1: int32(-1), v2: int32(2147483647)},
		{name: "Big", v1: int64(1), v2: int64(9223372036854775807)},
		{name: "Real", v1: float32(0.1), v2: float32(2.5), probes: []probe{{float32(0.2), 0, "another real"}}},
		{name: "Double", v1: 0.1, v2: 1e300},
		{name: "Bool", v1: true, v2: false},
		{name: "Mood", v1: model.Mood_Happy, v2: model.Mood_NeedsQuote},
		{name: "Date", v1: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), v2: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
			probes: []probe{{time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC), 1, "a date ignores the time of day"}}},
		{
			name:   "Ts",
			v1:     time.Date(2026, 3, 4, 10, 30, 0, 123456000, time.UTC),
			v2:     time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
			probes: []probe{{time.Date(2026, 3, 4, 10, 30, 0, 123457000, time.UTC), 0, "a microsecond apart"}},
		},
		{
			name: "Tstz",
			v1:   time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC),
			v2:   time.Date(2026, 3, 4, 8, 0, 0, 500000000, time.UTC),
			probes: []probe{
				{time.Date(2026, 3, 4, 10, 0, 0, 0, tz(2)), 1, "the same instant in another zone"},
				{time.Date(2026, 3, 4, 8, 0, 0, 0, tz(2)), 0, "the same wall clock in another zone is another instant"},
				{time.Date(2026, 3, 4, 3, 0, 0, 0, tz(-5)), 1, "the same instant, west of UTC"},
			},
		},
		{name: "Time", v1: time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC), v2: time.Date(0, 1, 1, 23, 59, 59, 0, time.UTC)},
		{name: "Timetz", v1: time.Date(0, 1, 1, 9, 0, 0, 0, tz(2)), v2: time.Date(0, 1, 1, 23, 59, 59, 0, time.UTC)},
	}
}

func seedKitchens(t *testing.T, db execer) {
	t.Helper()
	for o := range 2 {
		for k, pick := range []func(ktype) any{func(x ktype) any { return x.v1 }, func(x ktype) any { return x.v2 }} {
			args := []any{idOf(fmt.Sprintf("kitchen-%d-%d", o, k)), orgs[o]}
			for _, ty := range ktypes() {
				args = append(args, pick(ty))
			}
			for _, ty := range ktypes() { // the nullable twin: NULL in row 1
				if k == 0 {
					args = append(args, nil)
				} else {
					args = append(args, pick(ty))
				}
			}
			cols, marks := []string{"id", "org_id"}, []string{"$1", "$2"}
			for i, pre := range []string{"c_", "n_"} {
				for ti, ty := range ktypes() {
					cols = append(cols, pre+strings.ToLower(ty.name))
					mark := fmt.Sprintf("$%d", 3+i*len(ktypes())+ti)
					if ty.name == "Mood" {
						mark += "::mood"
					}
					marks = append(marks, mark)
				}
			}
			mustExec(
				t,
				db,
				`INSERT INTO kitchen (`+strings.Join(cols, ", ")+`, c_inet, c_json, c_bytes, c_texts, c_ints)
				VALUES (`+strings.Join(marks, ",")+`, '10.0.0.1', '{"a": 1}', '\x01', '{a,b}', '{1,2}')`,
				args...)
		}
	}
}

// call runs a method of repo by name, with ctx first.
func call(t *testing.T, repo any, method string, org uuid.UUID, v any) []reflect.Value {
	t.Helper()
	m := reflect.ValueOf(repo).MethodByName(method)
	if !m.IsValid() {
		t.Fatalf("no method %s", method)
	}
	return m.Call([]reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(org), reflect.ValueOf(v)})
}

func kitchenIDs(t *testing.T, out []reflect.Value) []uuid.UUID {
	t.Helper()
	if err, _ := reflect.TypeAssert[error](out[1]); err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	kitchens, _ := reflect.TypeAssert[[]model.Kitchen](out[0])
	for _, k := range kitchens {
		ids = append(ids, k.ID)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return ids
}

// Every type that has an equality predicate round-trips through a derived
// find, count, exists and delete, NOT NULL and nullable; a NULL never matches;
// another org never matches.
func TestKitchenTypesRoundTrip(t *testing.T) {
	db := openTorture(t)
	seedKitchens(t, db)
	repo := NewKitchens(db)
	row := func(o, k int) uuid.UUID { return idOf(fmt.Sprintf("kitchen-%d-%d", o, k)) }
	sorted := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
		return ids
	}
	for _, pre := range []string{"C", "N"} {
		for _, ty := range ktypes() {
			col := pre + ty.name
			t.Run(col, func(t *testing.T) {
				// k is the row index a value selects: row 0 holds v1, row 1 holds v2; the nullable twin is NULL in row 0.
				cases := []probe{{ty.v1, 1, "v1"}, {ty.v2, 2, "v2"}}
				cases = append(cases, ty.probes...)
				for _, c := range cases {
					for o := range 3 {
						var want []uuid.UUID
						if o < 2 && c.want > 0 && (pre != "N" || c.want != 1) {
							want = sorted(row(o, c.want-1))
						}
						if got := kitchenIDs(
							t,
							call(t, repo, "FindByOrgAnd"+col, orgs[o], c.v),
						); !slices.Equal(
							got,
							want,
						) {
							t.Errorf("%s org %d value %v (%s): found %v, want %v", col, o, c.v, c.why, got, want)
						}
						n := call(t, repo, "CountByOrgAnd"+col, orgs[o], c.v)
						if n[1].Interface() != nil || n[0].Int() != int64(len(want)) {
							t.Errorf("%s org %d value %v: count %v %v, want %d", col, o, c.v, n[0], n[1], len(want))
						}
						e := call(t, repo, "ExistsByOrgAnd"+col, orgs[o], c.v)
						if e[1].Interface() != nil || e[0].Bool() != (len(want) > 0) {
							t.Errorf(
								"%s org %d value %v: exists %v %v, want %v",
								col,
								o,
								c.v,
								e[0],
								e[1],
								len(want) > 0,
							)
						}
						deleteAndCheck(t, db, repo, "DeleteByOrgAnd"+col, orgs[o], c.v, want)
					}
				}
			})
		}
	}
}

// deleteAndCheck runs a derived delete in a transaction that it rolls back,
// and reads the table: exactly the wanted rows are gone, and every other
// row, of any org, is still there.
func deleteAndCheck(t *testing.T, db *sql.DB, repo any, method string, org uuid.UUID, v any, want []uuid.UUID) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	m := reflect.ValueOf(repo).MethodByName(method)
	out := m.Call(
		[]reflect.Value{
			reflect.ValueOf(transactor.WithTx(context.Background(), tx)),
			reflect.ValueOf(org),
			reflect.ValueOf(v),
		},
	)
	if ierr, _ := reflect.TypeAssert[error](out[1]); ierr != nil {
		t.Fatal(ierr)
	}
	if out[0].Int() != int64(len(want)) {
		t.Errorf("%s org %v value %v: deleted %d, want %d", method, org, v, out[0].Int(), len(want))
	}
	var left []uuid.UUID
	rows, err := tx.QueryContext(context.Background(), `SELECT id FROM kitchen`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	var keep []uuid.UUID
	for o := range 2 {
		for k := range 2 {
			if id := idOf(fmt.Sprintf("kitchen-%d-%d", o, k)); !slices.Contains(want, id) {
				keep = append(keep, id)
			}
		}
	}
	slices.SortFunc(keep, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	slices.SortFunc(left, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	if !slices.Equal(left, keep) {
		t.Errorf("%s org %v value %v: rows left %v, want %v: it deleted other rows", method, org, v, left, keep)
	}
}

// A value that is no member of the enum is a PostgreSQL error, never an empty
// result: the predicate casts the value to the enum type.
func TestUnknownEnumValueIsAnError(t *testing.T) {
	db := openTorture(t)
	seedKitchens(t, db)
	repo := NewKitchens(db)
	_, err := repo.FindByOrgAndCMood(context.Background(), orgs[0], model.Mood("NOPE"))
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "22P02" {
		t.Errorf("err = %v, want invalid_text_representation (22P02)", err)
	}
	if n, dErr := repo.DeleteByOrgAndCMood(context.Background(), orgs[0], model.Mood("NOPE")); dErr == nil || n != 0 {
		t.Errorf("DeleteByOrgAndCMood of an unknown value: %d, %v; want an error and no row deleted", n, dErr)
	}
	if n := len(column(t, db, `SELECT id::text FROM kitchen`)); n != 4 {
		t.Errorf("%d rows left, want 4", n)
	}
}
