// This file is Nanostack-only (not in go-jet): the db/keyset pager against
// PostgreSQL. It is the copy of pkg/db/transactor/keyset_integration_test.go
// that calls db/keyset directly. See db/UPSTREAM.md.

package postgres

import (
	"context"
	"fmt"
	"math"
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

// keysetDB is the suite's database. CockroachDB has no enum order and no inet
// type, and ADR 0015 drops it, so the keyset suite skips it.
func keysetDB(t *testing.T) qrm.DB {
	t.Helper()
	skipForCockroachDB(t)
	return db
}

func ksOrder(
	t *testing.T, sorts []search.Sort[ksField], keys map[ksField]keyset.Key[ksRow], tiebreak keyset.Key[ksRow],
) keyset.Order[ksRow] {
	t.Helper()
	order, err := keyset.OrderOf(sorts, keys, tiebreak)
	require.NoError(t, err)
	return order
}

// setupKeysetSchema creates the table of a test, and drops it when the test
// ends. The enum type is created once and kept: the pgx driver caches the
// statements of each connection, and a cached statement names the type by its
// OID, which a type created again would change.
func setupKeysetSchema(t *testing.T, conn qrm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_keyset`,
		// Declaration order differs from text order on purpose.
		`DO $$ BEGIN
			CREATE TYPE it_keyset_status AS ENUM ('pending', 'active', 'archived');
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$`,
		`CREATE TABLE it_keyset (
			id text PRIMARY KEY,
			tenant text NOT NULL,
			name text NOT NULL,
			created timestamptz NOT NULL,
			note text,
			status it_keyset_status NOT NULL,
			rank bigint NOT NULL,
			score double precision,
			seen timestamp,
			ref uuid NOT NULL,
			ip inet
		)`,
	} {
		if _, err := conn.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS it_keyset`)
	})
}

type ksGen struct {
	rng  *rand.Rand
	next int
	base time.Time
}

func (g *ksGen) insert(t *testing.T, conn qrm.DB, tenant string) {
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
	_, err := conn.ExecContext(t.Context(),
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
