// This file is Nanostack-only (not in go-jet): the computed keys of db/keyset in the
// cross-dialect contract (design-search S3.3, R10). See db/UPSTREAM.md.

package keysetcontract

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// computedRow is a row of the contract table with the aggregates of its children, which
// the computed keys read through their aliases. Some rows have no child, so every aggregate
// but COUNT is NULL there.
type computedRow struct {
	ID       string   `alias:"kc_rows.id"`
	Tenant   string   `alias:"kc_rows.tenant"`
	Name     string   `alias:"kc_rows.name"`
	Children int64    `alias:"kc_computed.children"`
	Biggest  *int64   `alias:"kc_computed.biggest"`
	Biggest0 int64    `alias:"kc_computed.biggest_or_zero"`
	Total    *float64 `alias:"kc_computed.total"`
	Total0   float64  `alias:"kc_computed.total_or_zero"`
	First    *string  `alias:"kc_computed.first_label"`
	Last     string   `alias:"kc_computed.last_label"`
}

// child is one row of the children table.
type child struct {
	row    string
	size   int64
	amount float64
	label  string
}

// computedKey is a computed key and its order in Go.
type computedKey struct {
	key   keyset.Key[computedRow]
	cmp   func(a, b computedRow) int
	null  func(computedRow) bool
	nulls string
}

// generateChildren gives each row zero to three children. Few distinct sizes, amounts and
// labels make ties dominate. The amounts are binary fractions, so every sum is exact.
func generateChildren(rows []Row) []child {
	//nolint:gosec // a fixed seed, so every dialect gets the same rows; nothing here is secret
	rng := rand.New(rand.NewPCG(2026, 12))
	var children []child
	for _, row := range rows {
		for range rng.IntN(4) {
			children = append(children, child{
				row:    row.ID,
				size:   int64(1 + rng.IntN(2)),
				amount: []float64{0.5, 1.25, 2}[rng.IntN(3)],
				label:  []string{"a", "b", "B"}[rng.IntN(3)],
			})
		}
	}
	return children
}

// aggregate computes in Go what the computed keys read in SQL.
func aggregate(rows []Row, children []child) []computedRow {
	out := make([]computedRow, len(rows))
	index := map[string]int{}
	for i, r := range rows {
		out[i] = computedRow{ID: r.ID, Tenant: r.Tenant, Name: r.Name}
		index[r.ID] = i
	}
	for _, ch := range children {
		r := &out[index[ch.row]]
		r.Children++
		if r.Biggest == nil || ch.size > *r.Biggest {
			r.Biggest = &ch.size
		}
		total := ch.amount
		if r.Total != nil {
			total += *r.Total
		}
		r.Total = &total
		if r.First == nil || ch.label < *r.First {
			r.First = &ch.label
		}
		r.Last = max(r.Last, ch.label)
	}
	for i := range out {
		if out[i].Biggest != nil {
			out[i].Biggest0 = *out[i].Biggest
		}
		if out[i].Total != nil {
			out[i].Total0 = *out[i].Total
		}
	}
	return out
}

func (c *contract) childrenDDL() []string {
	switch c.cfg.Flavor {
	case Postgres:
		return []string{
			`DROP TABLE IF EXISTS kc_children`,
			`CREATE TABLE kc_children (
				row_id text COLLATE "C" NOT NULL,
				size bigint NOT NULL,
				amount double precision NOT NULL,
				label text COLLATE "C" NOT NULL
			)`,
			`CREATE INDEX kc_children_row ON kc_children (row_id)`,
		}
	case MySQL, MariaDB:
		binary := "CHARACTER SET utf8mb4 COLLATE " + c.binaryCollation()
		return []string{
			`DROP TABLE IF EXISTS kc_children`,
			`CREATE TABLE kc_children (
				row_id VARCHAR(40) ` + binary + ` NOT NULL,
				size BIGINT NOT NULL,
				amount DOUBLE NOT NULL,
				label VARCHAR(8) ` + binary + ` NOT NULL,
				KEY kc_children_row (row_id)
			)`,
		}
	case SQLite:
		return []string{
			`DROP TABLE IF EXISTS kc_children`,
			`CREATE TABLE kc_children (
				row_id TEXT NOT NULL,
				size INTEGER NOT NULL,
				amount REAL NOT NULL,
				label TEXT NOT NULL
			)`,
			`CREATE INDEX kc_children_row ON kc_children (row_id)`,
		}
	}
	panic("keysetcontract: unknown flavor " + string(c.cfg.Flavor))
}

func (c *contract) insertChildren(t *testing.T, children []child) {
	t.Helper()
	for _, stmt := range c.childrenDDL() {
		_, err := c.cfg.DB.ExecContext(t.Context(), stmt)
		require.NoError(t, err, "setup %q", stmt)
	}
	t.Cleanup(func() {
		_, _ = c.cfg.DB.ExecContext(context.Background(), `DROP TABLE IF EXISTS kc_children`)
	})
	const batch = 100
	for start := 0; start < len(children); start += batch {
		chunk := children[start:min(start+batch, len(children))]
		tuples := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)*4)
		for i, ch := range chunk {
			tuples[i] = "(?, ?, ?, ?)"
			if c.cfg.Flavor == Postgres {
				tuples[i] = fmt.Sprintf("($%d, $%d, $%d, $%d)", i*4+1, i*4+2, i*4+3, i*4+4)
			}
			args = append(args, ch.row, ch.size, ch.amount, ch.label)
		}
		_, err := c.cfg.DB.ExecContext(t.Context(),
			"INSERT INTO kc_children (row_id, size, amount, label) VALUES "+strings.Join(tuples, ", "), args...)
		require.NoError(t, err)
	}
	if c.cfg.Flavor == MySQL || c.cfg.Flavor == MariaDB {
		// Fresh statistics for the just-filled tables: without them MariaDB plans
		// the correlated subqueries badly and the walks take 4 times longer
		// (46 s against 11 s for the whole contract, measured 2026-11-11).
		for _, table := range []string{"kc_rows", "kc_children"} {
			_, err := c.cfg.DB.ExecContext(t.Context(), "ANALYZE TABLE "+table)
			require.NoError(t, err)
		}
	}
}

// computedKeys are the computed keys over the children of each row, in the dialect's own
// subquery builder.
func (c *contract) computedKeys() map[string]computedKey {
	var (
		rowID  = postgres.StringColumn("row_id")
		size   = postgres.IntegerColumn("size")
		amount = postgres.FloatColumn("amount")
		label  = postgres.StringColumn("label")
	)
	children := c.cfg.NewTable(c.cfg.Schema, "kc_children", rowID, size, amount, label)
	of := func(projection postgres.Projection) postgres.Expression {
		return c.cfg.Subquery(children, projection, rowID.EQ(c.idCol))
	}
	biggest := func() keyset.Key[computedRow] {
		return keyset.NullableIntExprKey("kc_computed.biggest", postgres.IntExp(of(postgres.MAXi(size))),
			func(r computedRow) *int64 { return r.Biggest })
	}
	biggestCmp := func(a, b computedRow) int { return cmp.Compare(*a.Biggest, *b.Biggest) }
	noBiggest := func(r computedRow) bool { return r.Biggest == nil }
	return map[string]computedKey{
		"children": {
			key: keyset.IntExprKey("kc_computed.children", postgres.IntExp(of(postgres.COUNT(postgres.STAR))),
				func(r computedRow) int64 { return r.Children }),
			cmp: func(a, b computedRow) int { return cmp.Compare(a.Children, b.Children) },
		},
		"biggest":             {key: biggest(), cmp: biggestCmp, null: noBiggest},
		"biggest_nulls_first": {key: biggest().NullsFirst(), cmp: biggestCmp, null: noBiggest, nulls: "first"},
		"biggest_nulls_last":  {key: biggest().NullsLast(), cmp: biggestCmp, null: noBiggest, nulls: "last"},
		// A NOT NULL form over an expression that can be NULL reads NULL as 0.
		"biggest_or_zero": {
			key: keyset.IntExprKey("kc_computed.biggest_or_zero", postgres.IntExp(of(postgres.MAXi(size))),
				func(r computedRow) int64 { return r.Biggest0 }),
			cmp: func(a, b computedRow) int { return cmp.Compare(a.Biggest0, b.Biggest0) },
		},
		"total": {
			key: keyset.NullableFloatExprKey("kc_computed.total", postgres.FloatExp(of(postgres.SUMf(amount))),
				func(r computedRow) *float64 { return r.Total }),
			cmp:  func(a, b computedRow) int { return cmp.Compare(*a.Total, *b.Total) },
			null: func(r computedRow) bool { return r.Total == nil },
		},
		"total_or_zero": {
			key: keyset.FloatExprKey("kc_computed.total_or_zero", postgres.FloatExp(of(postgres.SUMf(amount))),
				func(r computedRow) float64 { return r.Total0 }),
			cmp: func(a, b computedRow) int { return cmp.Compare(a.Total0, b.Total0) },
		},
		"first_label": {
			key: keyset.NullableStringExprKey("kc_computed.first_label", postgres.StringExp(of(postgres.MIN(label))),
				func(r computedRow) *string { return r.First }),
			cmp:  func(a, b computedRow) int { return strings.Compare(*a.First, *b.First) },
			null: func(r computedRow) bool { return r.First == nil },
		},
		"last_label": {
			key: keyset.StringExprKey("kc_computed.last_label", postgres.StringExp(of(postgres.MAX(label))),
				func(r computedRow) string { return r.Last }),
			cmp: func(a, b computedRow) int { return strings.Compare(a.Last, b.Last) },
		},
		"name": {
			key: keyset.StringKey(c.nameCol, func(r computedRow) string { return r.Name }),
			cmp: func(a, b computedRow) int { return strings.Compare(a.Name, b.Name) },
		},
	}
}

// computedSearches are a walk per computed key and direction, and walks with a second key.
func computedSearches() []spec {
	asc, desc := search.SortAscending, search.SortDescending
	var out []spec
	for _, field := range []string{
		"children", "biggest", "biggest_nulls_first", "biggest_nulls_last", "biggest_or_zero", "total",
		"total_or_zero", "first_label", "last_label",
	} {
		out = append(out,
			spec{sorts: []search.Sort[string]{by(field, asc)}},
			spec{sorts: []search.Sort[string]{by(field, desc)}, tenant: "t1"})
	}
	out = append(out,
		spec{sorts: []search.Sort[string]{by("children", desc), by("name", asc)}},
		spec{sorts: []search.Sort[string]{by("biggest", desc), by("total", asc)}, tenant: "t2"},
		spec{sorts: []search.Sort[string]{by("first_label", asc), by("children", desc)}},
		spec{sorts: []search.Sort[string]{by("name", desc), by("total", desc)}},
	)
	for i := range out {
		out[i].limit = limits[i%3] // 1, 3 and 7: every row, the NULL ones too, is a page boundary
	}
	return out
}

// checkComputed walks every computed key on the contract rows and their children. The walk
// returns the rows in the order that Go computes, with the cursor tokens that Go computes,
// so the tokens are the same on every dialect. Each scanned row carries the values that Go
// computes, read through the aliases of the keys.
func (c *contract) checkComputed(t *testing.T) {
	children := generateChildren(c.rows)
	c.insertChildren(t, children)
	rows := aggregate(c.rows, children)
	keys := c.computedKeys()
	byID := map[string]computedRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	// id, tenant and name, and each computed value: Page does not select a computed key of
	// the order a second time.
	columns := slices.Clone(c.columns[:3])
	for _, field := range []string{
		"children", "biggest", "biggest_or_zero", "total", "total_or_zero", "first_label", "last_label",
	} {
		columns = append(columns, keys[field].key.Projection())
	}
	for i, s := range computedSearches() {
		t.Run(fmt.Sprintf("%02d %s", i, s), func(t *testing.T) {
			order, want := c.computedOrder(t, keys, rows, s)
			var wantTokens []string
			for end := int(s.limit); end < len(want); end += int(s.limit) {
				token, err := order.Cursor(want[end-1])
				require.NoError(t, err)
				wantTokens = append(wantTokens, token)
			}
			var got, tokens []string
			page := search.Pagination{Limit: s.limit}
			for range len(rows) + 2 {
				result, err := keyset.Page(c.cfg.DB, func(r computedRow) computedRow { return r }, columns...).
					From(c.source).Where(c.where(s)).Keyset(order).Run(t.Context(), page)
				require.NoError(t, err)
				require.Equal(t, int64(len(want)), result.Total)
				for _, r := range result.Items {
					require.Equal(t, byID[r.ID], r, "the scan reads each value through its alias")
					got = append(got, r.ID)
				}
				if result.NextCursor == "" {
					break
				}
				tokens = append(tokens, result.NextCursor)
				page.Cursor = result.NextCursor
			}
			wantIDs := make([]string, len(want))
			for i, r := range want {
				wantIDs[i] = r.ID
			}
			require.Equal(t, wantIDs, got, "the walk returns the rows in the order computed in Go")
			require.Equal(t, wantTokens, tokens, "the cursor tokens are the ones computed in Go")
		})
	}
}

// computedOrder returns the keyset order of s and the matching rows sorted in Go.
func (c *contract) computedOrder(
	t *testing.T, keys map[string]computedKey, rows []computedRow, s spec,
) (keyset.Order[computedRow], []computedRow) {
	t.Helper()
	orderKeys := make(map[string]keyset.Key[computedRow], len(keys))
	for field, def := range keys {
		orderKeys[field] = def.key
	}
	id := computedKey{
		key: keyset.StringKey(c.idCol, func(r computedRow) string { return r.ID }),
		cmp: func(a, b computedRow) int { return strings.Compare(a.ID, b.ID) },
	}
	order, err := keyset.OrderOf(s.sorts, orderKeys, id.key)
	require.NoError(t, err)

	var want []computedRow
	for _, r := range rows {
		if s.tenant == "" || r.Tenant == s.tenant {
			want = append(want, r)
		}
	}
	direction := search.SortAscending
	type step struct {
		def       computedKey
		direction search.SortDirection
	}
	var steps []step
	for _, sort := range s.sorts {
		direction = sort.Direction
		steps = append(steps, step{keys[sort.Field], sort.Direction})
	}
	steps = append(steps, step{id, direction})
	slices.SortStableFunc(want, func(a, b computedRow) int {
		for _, st := range steps {
			if r := compareComputed(st.def, st.direction, a, b); r != 0 {
				return r
			}
		}
		return 0
	})
	return order, want
}

func compareComputed(def computedKey, direction search.SortDirection, a, b computedRow) int {
	if def.null != nil {
		aNull, bNull := def.null(a), def.null(b)
		nullsAfter := def.nulls == "last" || (def.nulls == "" && direction == search.SortAscending)
		switch {
		case aNull && bNull:
			return 0
		case aNull && nullsAfter, bNull && !nullsAfter:
			return 1
		case aNull, bNull:
			return -1
		}
	}
	r := def.cmp(a, b)
	if direction == search.SortDescending {
		return -r
	}
	return r
}
