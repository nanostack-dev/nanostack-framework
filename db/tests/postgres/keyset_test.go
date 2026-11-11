// This file is Nanostack-only (not in go-jet): the db/keyset pager against
// PostgreSQL. It is the copy of pkg/db/transactor/keyset_integration_test.go
// that calls db/keyset directly. See db/UPSTREAM.md.

package postgres

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// The keyset tests check the property that makes cursors worth having: for
// every ordering, walking the cursors returns exactly the rows of one ordered
// query, in that order, and keeps doing so while rows are written between
// pages. The data is built to break naive implementations: few distinct sort
// values so ties dominate, NULLs in the nullable columns, an enum whose
// declaration order differs from its text order, and timestamps that differ
// only in microseconds.

type ksField string

const (
	ksName    ksField = "NAME"
	ksCreated ksField = "CREATED"
	ksNote    ksField = "NOTE"
	ksStatus  ksField = "STATUS"
	ksRank    ksField = "RANK"
	ksScore   ksField = "SCORE"
	ksSeen    ksField = "SEEN"
	ksRef     ksField = "REF"
	ksNoteNL  ksField = "NOTE_NULLS_LAST"
	ksScoreNF ksField = "SCORE_NULLS_FIRST"
	ksIP      ksField = "IP"
)

var ksStatusLabels = []string{"pending", "active", "archived"}

type ksRow struct {
	ID      string     `alias:"it_keyset.id"`
	Tenant  string     `alias:"it_keyset.tenant"`
	Name    string     `alias:"it_keyset.name"`
	Created time.Time  `alias:"it_keyset.created"`
	Note    *string    `alias:"it_keyset.note"`
	Status  string     `alias:"it_keyset.status"`
	Rank    int64      `alias:"it_keyset.rank"`
	Score   *float64   `alias:"it_keyset.score"`
	Seen    *time.Time `alias:"it_keyset.seen"`
	Ref     uuid.UUID  `alias:"it_keyset.ref"`
	IP      *string    `alias:"it_keyset.ip"`
}

var (
	ksID       = jet.StringColumn("id")
	ksTenantC  = jet.StringColumn("tenant")
	ksNameC    = jet.StringColumn("name")
	ksCreatedC = jet.TimestampzColumn("created")
	ksNoteC    = jet.StringColumn("note")
	ksStatusC  = jet.StringColumn("status")
	ksRankC    = jet.IntegerColumn("rank")
	ksScoreC   = jet.FloatColumn("score")
	ksSeenC    = jet.TimestampColumn("seen")
	ksRefC     = jet.StringColumn("ref")
	ksIPC      = jet.StringColumn("ip")
	ksTable    = jet.NewTable("public", "it_keyset", "",
		ksID, ksTenantC, ksNameC, ksCreatedC, ksNoteC, ksStatusC, ksRankC, ksScoreC, ksSeenC, ksRefC, ksIPC)
	ksAll = []jet.Projection{
		ksID,
		ksTenantC,
		ksNameC,
		ksCreatedC,
		ksNoteC,
		ksStatusC,
		ksRankC,
		ksScoreC,
		ksSeenC,
		ksRefC,
		ksIPC,
	}
)

func ksKeys() map[ksField]keyset.Key[ksRow] {
	return map[ksField]keyset.Key[ksRow]{
		ksName:    keyset.StringKey(ksNameC, func(r ksRow) string { return r.Name }),
		ksCreated: keyset.TimeKey(ksCreatedC, func(r ksRow) time.Time { return r.Created }),
		ksNote:    keyset.NullableStringKey(ksNoteC, func(r ksRow) *string { return r.Note }),
		ksStatus: keyset.EnumKey(
			ksStatusC,
			"it_keyset_status",
			ksStatusLabels,
			func(r ksRow) string { return r.Status },
		),
		ksRank:    keyset.IntKey(ksRankC, func(r ksRow) int64 { return r.Rank }),
		ksScore:   keyset.NullableFloatKey(ksScoreC, func(r ksRow) *float64 { return r.Score }),
		ksSeen:    keyset.NullableLocalTimeKey(ksSeenC, func(r ksRow) *time.Time { return r.Seen }),
		ksRef:     keyset.UUIDKey(ksRefC, func(r ksRow) uuid.UUID { return r.Ref }),
		ksNoteNL:  keyset.NullableStringKey(ksNoteC, func(r ksRow) *string { return r.Note }).NullsLast(),
		ksScoreNF: keyset.NullableFloatKey(ksScoreC, func(r ksRow) *float64 { return r.Score }).NullsFirst(),
		ksIP:      keyset.NullableInetKey(ksIPC, func(r ksRow) *string { return r.IP }),
	}
}

func ksTiebreak() keyset.Key[ksRow] {
	return keyset.StringKey(ksID, func(r ksRow) string { return r.ID })
}

func ksPage(t *testing.T, conn qrm.DB, order keyset.Order[ksRow], page search.Pagination) search.Result[ksRow] {
	t.Helper()
	result, err := keyset.Page(conn, func(r ksRow) ksRow { return r }, ksAll...).
		From(ksTable).
		Where(ksTenantC.EQ(jet.String("t1"))).
		Keyset(order).
		Run(t.Context(), page)
	if err != nil {
		t.Fatalf("Run(%+v): %v", page, err)
	}
	return result
}

func ksOrdered(t *testing.T, conn qrm.DB, order keyset.Order[ksRow]) []string {
	t.Helper()
	var rows []ksRow
	err := ksTable.SELECT(ksAll[0], ksAll[1:]...).
		WHERE(ksTenantC.EQ(jet.String("t1"))).
		ORDER_BY(order.OrderBy()...).
		QueryContext(t.Context(), conn, &rows)
	if err != nil {
		t.Fatalf("ordered query: %v", err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func ksWalk(t *testing.T, conn qrm.DB, order keyset.Order[ksRow], limit int32, between func()) []string {
	t.Helper()
	var ids []string
	page := search.Pagination{Limit: limit}
	for range 10_000 {
		result := ksPage(t, conn, order, page)
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
	conn := keysetDB(t)
	setupKeysetSchema(t, conn)
	gen := &ksGen{rng: rand.New(rand.NewPCG(1, 2)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 157 {
		gen.insert(t, conn, "t1")
	}
	for range 20 {
		gen.insert(t, conn, "t2") // other tenant, must never appear
	}

	for _, sorts := range ksOrderings() {
		order := ksOrder(t, sorts, ksKeys(), ksTiebreak())
		want := ksOrdered(t, conn, order)
		limits := []int32{7, 50, 500}
		if len(sorts) < 2 {
			limits = append(limits, 1) // every row is a page boundary
		}
		for _, limit := range limits {
			if got := ksWalk(t, conn, order, limit, nil); !slices.Equal(got, want) {
				t.Fatalf("%s limit %d: cursor walk returned %d rows that differ from the ordered query's %d",
					order.Signature(), limit, len(got), len(want))
			}
		}
	}
}

func TestIntegrationKeysetTotalAndOffsetHandoff(t *testing.T) {
	conn := keysetDB(t)
	setupKeysetSchema(t, conn)
	gen := &ksGen{rng: rand.New(rand.NewPCG(3, 4)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 40 {
		gen.insert(t, conn, "t1")
	}
	order := ksOrder(t,
		[]search.Sort[ksField]{{Field: ksName, Direction: search.SortDescending}},
		ksKeys(),
		ksTiebreak(),
	)
	want := ksOrdered(t, conn, order)

	// Offset pages still hand out a cursor, so a client can switch mid-walk.
	first := ksPage(t, conn, order, search.Pagination{Limit: 10, Offset: 10})
	if first.Total != 40 || first.Count != 10 || !first.HasMore() {
		t.Fatalf("offset page = total %d count %d more %v", first.Total, first.Count, first.HasMore())
	}
	rest := ksPage(t, conn, order, search.Pagination{Limit: 100, Cursor: first.NextCursor})
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
	conn := keysetDB(t)
	setupKeysetSchema(t, conn)
	gen := &ksGen{rng: rand.New(rand.NewPCG(5, 6)), base: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	for range 120 {
		gen.insert(t, conn, "t1")
	}

	for _, sorts := range [][]search.Sort[ksField]{
		{{Field: ksCreated, Direction: search.SortDescending}},
		{{Field: ksNote, Direction: search.SortAscending}, {Field: ksName, Direction: search.SortDescending}},
		{{Field: ksStatus, Direction: search.SortAscending}},
	} {
		order := ksOrder(t, sorts, ksKeys(), ksTiebreak())
		initial := ksOrdered(t, conn, order)
		deleted := map[string]bool{}
		mutate := func() {
			gen.insert(t, conn, "t1")
			// Delete a random existing row: sometimes already returned,
			// sometimes still ahead of the cursor.
			picked, err := query.One[string](t.Context(), conn,
				jet.RawStatement(`SELECT id FROM it_keyset WHERE tenant = 't1' ORDER BY random() LIMIT 1`))
			if err != nil || !picked.IsPresent() {
				t.Fatalf("pick victim: %v", err)
			}
			victim := picked.Value()
			if _, err = conn.ExecContext(t.Context(), `DELETE FROM it_keyset WHERE id = $1`, victim); err != nil {
				t.Fatalf("delete: %v", err)
			}
			deleted[victim] = true
		}

		got := ksWalk(t, conn, order, 9, mutate)

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
		final := ksOrdered(t, conn, order)
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
	conn := keysetDB(t)
	setupKeysetSchema(t, conn)
	gen := &ksGen{rng: rand.New(rand.NewPCG(7, 8)), base: time.Now()}
	for range 5 {
		gen.insert(t, conn, "t1")
	}
	byName := ksOrder(t,
		[]search.Sort[ksField]{{Field: ksName, Direction: search.SortAscending}},
		ksKeys(),
		ksTiebreak(),
	)
	byRank := ksOrder(t,
		[]search.Sort[ksField]{{Field: ksRank, Direction: search.SortAscending}},
		ksKeys(),
		ksTiebreak(),
	)
	first := ksPage(t, conn, byName, search.Pagination{Limit: 2})
	_, err := keyset.Page(conn, func(r ksRow) ksRow { return r }, ksAll...).
		From(ksTable).Keyset(byRank).
		Run(t.Context(), search.Pagination{Limit: 2, Cursor: first.NextCursor})
	require.ErrorIs(t, err, search.ErrInvalidCursor)
}
