//go:build integration

package transactor_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// The keyset tests check the property that makes cursors worth having: for
// every ordering, walking the cursors returns exactly the rows of one ordered
// query, in that order, and keeps doing so while rows are written between
// pages. The data is built to break naive implementations: few distinct sort
// values so ties dominate, NULLs in the nullable columns, an enum whose
// declaration order differs from its text order, and timestamps that differ
// only in microseconds.

func ksKeys() map[ksField]transactor.Key[ksRow] {
	//nolint:exhaustive // ksHandRank is a hand-written sort of the search tests
	return map[ksField]transactor.Key[ksRow]{
		ksName:    transactor.StringKey(ksNameC, func(r ksRow) string { return r.Name }),
		ksCreated: transactor.TimeKey(ksCreatedC, func(r ksRow) time.Time { return r.Created }),
		ksNote:    transactor.NullableStringKey(ksNoteC, func(r ksRow) *string { return r.Note }),
		ksStatus: transactor.EnumKey(
			ksStatusC,
			"it_keyset_status",
			ksStatusLabels,
			func(r ksRow) string { return r.Status },
		),
		ksRank:    transactor.IntKey(ksRankC, func(r ksRow) int64 { return r.Rank }),
		ksScore:   transactor.NullableFloatKey(ksScoreC, func(r ksRow) *float64 { return r.Score }),
		ksSeen:    transactor.NullableLocalTimeKey(ksSeenC, func(r ksRow) *time.Time { return r.Seen }),
		ksRef:     transactor.UUIDKey(ksRefC, func(r ksRow) uuid.UUID { return r.Ref }),
		ksNoteNL:  transactor.NullableStringKey(ksNoteC, func(r ksRow) *string { return r.Note }).NullsLast(),
		ksScoreNF: transactor.NullableFloatKey(ksScoreC, func(r ksRow) *float64 { return r.Score }).NullsFirst(),
		ksIP:      transactor.NullableInetKey(ksIPC, func(r ksRow) *string { return r.IP }),
	}
}

func ksTiebreak() transactor.Key[ksRow] {
	return transactor.StringKey(ksID, func(r ksRow) string { return r.ID })
}

type ksGen struct {
	rng  *rand.Rand
	next int
	base time.Time
}

func (g *ksGen) insert(t *testing.T, db *sql.DB, tenant string) {
	t.Helper()
	g.next++
	// Random ids, not sequential, so the tiebreak order is unrelated to
	// insertion order.
	id := fmt.Sprintf("w_%08x_%d", g.rng.Uint32(), g.next)
	var note *string
	if g.rng.IntN(5) < 2 {
		n := []string{"alpha", "beta", "Gamma"}[g.rng.IntN(3)]
		note = &n
	}
	var score *float64
	if g.rng.IntN(3) > 0 {
		s := []float64{-1.5, 0, 0.1, 2.25, math.NaN(), math.Inf(1), math.Inf(-1)}[g.rng.IntN(7)]
		score = &s
	}
	var seen *time.Time
	if g.rng.IntN(2) == 0 {
		s := g.base.Add(time.Duration(g.rng.IntN(3)) * time.Hour)
		seen = &s
	}
	// Few distinct refs so the uuid key has ties too.
	ref := []string{
		"018f6b8e-0000-7000-8000-000000000001",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"00000000-0000-4000-8000-00000000000a",
	}[g.rng.IntN(3)]
	var ip *string
	if g.rng.IntN(4) > 0 {
		// IPv4 sorts before IPv6 in inet order, unlike text order.
		v := []string{"10.0.0.2", "9.255.0.1", "::1", "10.0.0.0/8", "2001:db8::1"}[g.rng.IntN(5)]
		ip = &v
	}
	_, err := db.Exec(
		`INSERT INTO it_keyset (id, tenant, name, created, note, status, rank, score, seen, ref, ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, tenant,
		[]string{"apple", "Banana", "cherry", "apple ", "é"}[g.rng.IntN(5)],
		g.base.Add(time.Duration(g.rng.IntN(4))*time.Microsecond),
		note,
		[]string{"pending", "active", "archived"}[g.rng.IntN(3)],
		int64(g.rng.IntN(3)),
		score, seen, ref, ip,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func ksPage(t *testing.T, db *sql.DB, order transactor.Order[ksRow], page search.Pagination) search.Result[ksRow] {
	t.Helper()
	result, err := transactor.Page(db, func(r ksRow) ksRow { return r }, ksAll...).
		From(ksTable).
		Where(ksTenantC.EQ(jet.String("t1"))).
		Keyset(order).
		Run(context.Background(), page).
		Value()
	if err != nil {
		t.Fatalf("Run(%+v): %v", page, err)
	}
	return result
}

func ksOrdered(t *testing.T, db *sql.DB, order transactor.Order[ksRow]) []string {
	t.Helper()
	var rows []ksRow
	err := ksTable.SELECT(ksAll[0], ksAll[1:]...).
		WHERE(ksTenantC.EQ(jet.String("t1"))).
		ORDER_BY(order.OrderBy()...).
		QueryContext(context.Background(), db, &rows)
	if err != nil {
		t.Fatalf("ordered query: %v", err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func ksWalk(t *testing.T, db *sql.DB, order transactor.Order[ksRow], limit int32, between func()) []string {
	t.Helper()
	var ids []string
	page := search.Pagination{Limit: limit}
	for range 10_000 {
		result := ksPage(t, db, order, page)
		for _, r := range result.Items {
			ids = append(ids, r.ID)
		}
		if !result.HasMore() {
			return ids
		}
		if int32(result.Count) != limit {
			t.Fatalf("a page with a next cursor held %d rows, want %d", result.Count, limit)
		}
		page.Cursor = result.NextCursor
		if between != nil {
			between()
		}
	}
	t.Fatal("cursor walk did not terminate")
	return nil
}

func ksOrderings() [][]search.Sort[ksField] {
	fields := []ksField{ksName, ksCreated, ksNote, ksStatus, ksRank, ksScore, ksSeen, ksRef, ksNoteNL, ksScoreNF, ksIP}
	dirs := []search.SortDirection{search.SortAscending, search.SortDescending}
	orderings := [][]search.Sort[ksField]{nil}
	for _, f := range fields {
		for _, d := range dirs {
			orderings = append(orderings, []search.Sort[ksField]{{Field: f, Direction: d}})
		}
	}
	for _, f1 := range fields {
		for _, f2 := range fields {
			if f1 == f2 {
				continue
			}
			for _, d1 := range dirs {
				for _, d2 := range dirs {
					orderings = append(
						orderings,
						[]search.Sort[ksField]{{Field: f1, Direction: d1}, {Field: f2, Direction: d2}},
					)
				}
			}
		}
	}
	orderings = append(orderings, []search.Sort[ksField]{
		{Field: ksNote, Direction: search.SortDescending},
		{Field: ksScore, Direction: search.SortAscending},
		{Field: ksSeen, Direction: search.SortDescending},
	})
	return orderings
}

func TestIntegrationKeysetWalkMatchesOrderedQuery(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	gen := &ksGen{rng: rand.New(rand.NewPCG(1, 2)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 157 {
		gen.insert(t, db, "t1")
	}
	for range 20 {
		gen.insert(t, db, "t2") // other tenant, must never appear
	}

	for _, sorts := range ksOrderings() {
		order := transactor.Keys(sorts, ksKeys(), ksTiebreak())
		want := ksOrdered(t, db, order)
		for _, limit := range []int32{1, 7, 50, 157, 500} {
			if got := ksWalk(t, db, order, limit, nil); !slices.Equal(got, want) {
				t.Fatalf("%s limit %d: cursor walk returned %d rows that differ from the ordered query's %d",
					order.Signature(), limit, len(got), len(want))
			}
		}
	}
}

func TestIntegrationKeysetTotalAndOffsetHandoff(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	gen := &ksGen{rng: rand.New(rand.NewPCG(3, 4)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 40 {
		gen.insert(t, db, "t1")
	}
	order := transactor.Keys(
		[]search.Sort[ksField]{{Field: ksName, Direction: search.SortDescending}},
		ksKeys(),
		ksTiebreak(),
	)
	want := ksOrdered(t, db, order)

	// Offset pages still hand out a cursor, so a client can switch mid-walk.
	first := ksPage(t, db, order, search.Pagination{Limit: 10, Offset: 10})
	if first.Total != 40 || first.Count != 10 || !first.HasMore() {
		t.Fatalf("offset page = total %d count %d more %v", first.Total, first.Count, first.HasMore())
	}
	rest := ksPage(t, db, order, search.Pagination{Limit: 100, Cursor: first.NextCursor})
	if rest.Total != 40 {
		t.Fatalf("Total after a cursor = %d, want every match (40)", rest.Total)
	}
	var got []string
	for _, r := range append(first.Items, rest.Items...) {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, want[10:]) {
		t.Fatal("offset page followed by its cursor did not continue the ordering")
	}
	if rest.HasMore() {
		t.Fatal("the last page returned a next cursor")
	}
}

func TestIntegrationKeysetStableUnderWrites(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	gen := &ksGen{rng: rand.New(rand.NewPCG(5, 6)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 120 {
		gen.insert(t, db, "t1")
	}

	for _, sorts := range [][]search.Sort[ksField]{
		{{Field: ksCreated, Direction: search.SortDescending}},
		{{Field: ksNote, Direction: search.SortAscending}, {Field: ksName, Direction: search.SortDescending}},
		{{Field: ksStatus, Direction: search.SortAscending}},
	} {
		order := transactor.Keys(sorts, ksKeys(), ksTiebreak())
		initial := ksOrdered(t, db, order)
		deleted := map[string]bool{}
		mutate := func() {
			gen.insert(t, db, "t1")
			// Delete a random existing row: sometimes already returned,
			// sometimes still ahead of the cursor.
			var victim string
			if err := db.QueryRow(`SELECT id FROM it_keyset WHERE tenant = 't1' ORDER BY random() LIMIT 1`).
				Scan(&victim); err != nil {
				t.Fatalf("pick victim: %v", err)
			}
			if _, err := db.Exec(`DELETE FROM it_keyset WHERE id = $1`, victim); err != nil {
				t.Fatalf("delete: %v", err)
			}
			deleted[victim] = true
		}

		got := ksWalk(t, db, order, 9, mutate)

		seen := map[string]bool{}
		for _, id := range got {
			if seen[id] {
				t.Fatalf("%s: row %s returned twice", order.Signature(), id)
			}
			seen[id] = true
		}
		// Every row present for the whole walk is returned exactly once.
		for _, id := range initial {
			if !deleted[id] && !seen[id] {
				t.Fatalf("%s: surviving row %s was skipped", order.Signature(), id)
			}
		}
		// The rows returned follow the ordering, judged on the final data.
		final := ksOrdered(t, db, order)
		position := map[string]int{}
		for i, id := range final {
			position[id] = i
		}
		last := -1
		for _, id := range got {
			p, ok := position[id]
			if !ok {
				continue // deleted after it was returned
			}
			if p < last {
				t.Fatalf("%s: row %s returned out of order", order.Signature(), id)
			}
			last = p
		}
	}
}

func TestIntegrationKeysetRejectsCursorFromOtherSort(t *testing.T) {
	db := openDB(t)
	setupKeysetSchema(t, db)
	gen := &ksGen{rng: rand.New(rand.NewPCG(7, 8)), base: time.Now()}
	for range 5 {
		gen.insert(t, db, "t1")
	}
	byName := transactor.Keys(
		[]search.Sort[ksField]{{Field: ksName, Direction: search.SortAscending}},
		ksKeys(),
		ksTiebreak(),
	)
	byRank := transactor.Keys(
		[]search.Sort[ksField]{{Field: ksRank, Direction: search.SortAscending}},
		ksKeys(),
		ksTiebreak(),
	)
	first := ksPage(t, db, byName, search.Pagination{Limit: 2})
	_, err := transactor.Page(db, func(r ksRow) ksRow { return r }, ksAll...).
		From(ksTable).Keyset(byRank).
		Run(context.Background(), search.Pagination{Limit: 2, Cursor: first.NextCursor}).Value()
	if err == nil {
		t.Fatal("a cursor from another sort was accepted")
	}
}
