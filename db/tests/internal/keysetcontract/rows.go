// This file is Nanostack-only (not in go-jet): the keys and the rows of the
// db/keyset contract test. See db/UPSTREAM.md.

package keysetcontract

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// keyDef is a key and the Go comparison that the contract expects of it.
type keyDef struct {
	key keyset.Key[Row]
	// cmp compares two non-NULL values.
	cmp func(a, b Row) int
	// null reports a NULL value. nil for a NOT NULL key.
	null func(Row) bool
	// nulls is "", "first" or "last", as the key asks.
	nulls string
}

func strCmp(get func(Row) string) func(a, b Row) int {
	return func(a, b Row) int { return strings.Compare(get(a), get(b)) }
}

func ptrCmp[V any](get func(Row) *V, compare func(a, b V) int) func(a, b Row) int {
	return func(a, b Row) int { return compare(*get(a), *get(b)) }
}

func isNil[V any](get func(Row) *V) func(Row) bool {
	return func(r Row) bool { return get(r) == nil }
}

func labelCmp(a, b string) int { return cmp.Compare(slices.Index(Labels, a), slices.Index(Labels, b)) }

func contractKeys(
	name, nameCI, note, noteCI, status, statusP, ref postgres.ColumnString,
	points, pointsP postgres.ColumnInteger,
	score, ratio postgres.ColumnFloat,
	created, seen, stamp postgres.ColumnTimestamp,
) map[string]keyDef {
	noteOf := func(r Row) *string { return r.Note }
	noteCIOf := func(r Row) *string { return r.NoteCI }
	statusPOf := func(r Row) *string { return r.StatusP }
	pointsPOf := func(r Row) *int64 { return r.PointsP }
	scoreOf := func(r Row) *float64 { return r.Score }
	seenOf := func(r Row) *time.Time { return r.Seen }
	stampOf := func(r Row) *time.Time { return r.Stamp }
	return map[string]keyDef{
		"name": {key: keyset.StringKey(name, func(r Row) string { return r.Name }), cmp: strCmp(func(r Row) string {
			return r.Name
		})},
		"name_ci": {key: keyset.StringKey(nameCI, func(r Row) string { return r.NameCI })},
		"note": {
			key: keyset.NullableStringKey(note, noteOf), cmp: ptrCmp(noteOf, strings.Compare), null: isNil(noteOf),
		},
		"note_nulls_first": {
			key: keyset.NullableStringKey(note, noteOf).NullsFirst(), cmp: ptrCmp(noteOf, strings.Compare),
			null: isNil(noteOf), nulls: "first",
		},
		"note_nulls_last": {
			key: keyset.NullableStringKey(note, noteOf).NullsLast(), cmp: ptrCmp(noteOf, strings.Compare),
			null: isNil(noteOf), nulls: "last",
		},
		"note_ci": {key: keyset.NullableStringKey(noteCI, noteCIOf), null: isNil(noteCIOf)},
		"status": {
			key: keyset.EnumKey(status, "kc_status", Labels, func(r Row) string { return r.Status }),
			cmp: func(a, b Row) int { return labelCmp(a.Status, b.Status) },
		},
		"status_p": {
			key: keyset.NullableEnumKey(statusP, "kc_status", Labels, statusPOf),
			cmp: ptrCmp(statusPOf, labelCmp), null: isNil(statusPOf),
		},
		"ref": {
			key: keyset.UUIDKey(ref, func(r Row) uuid.UUID { return r.Ref }),
			cmp: func(a, b Row) int { return strings.Compare(a.Ref.String(), b.Ref.String()) },
		},
		"points": {
			key: keyset.IntKey(points, func(r Row) int64 { return r.Points }),
			cmp: func(a, b Row) int { return cmp.Compare(a.Points, b.Points) },
		},
		"points_p": {
			key: keyset.NullableIntKey(pointsP, pointsPOf), cmp: ptrCmp(pointsPOf, cmp.Compare[int64]),
			null: isNil(pointsPOf),
		},
		"score": {
			key: keyset.NullableFloatKey(score, scoreOf), cmp: ptrCmp(scoreOf, cmp.Compare[float64]),
			null: isNil(scoreOf),
		},
		"ratio": {
			key: keyset.RealKey(ratio, func(r Row) float32 { return r.Ratio }),
			cmp: func(a, b Row) int { return cmp.Compare(a.Ratio, b.Ratio) },
		},
		"created": {
			key: keyset.LocalTimeKey(created, func(r Row) time.Time { return r.Created }),
			cmp: func(a, b Row) int { return a.Created.Compare(b.Created) },
		},
		"seen": {
			key: keyset.NullableLocalTimeKey(seen, seenOf), cmp: ptrCmp(seenOf, time.Time.Compare),
			null: isNil(seenOf),
		},
		"stamp": {
			key: keyset.NullableMySQLTimestampKey(stamp, stampOf), cmp: ptrCmp(stampOf, time.Time.Compare),
			null: isNil(stampOf),
		},
	}
}

// generateRows builds the same 200 rows on every run: few distinct values so
// ties dominate, NULLs in every nullable column, mixed case, the characters
// % _ ! \ and non-ASCII letters in text, and times that differ in
// microseconds only.
func generateRows() []Row {
	//nolint:gosec // a fixed seed, so every dialect gets the same rows; nothing here is secret
	rng := rand.New(rand.NewPCG(2026, 11))
	pick := func(values ...string) string { return values[rng.IntN(len(values))] }
	maybe := func(percent int) bool { return rng.IntN(100) < percent }
	texts := []string{
		"apple",
		"Apple",
		"APPLE",
		"banana",
		"Banana",
		"a%b",
		"a_b",
		"a!b",
		`a\b`,
		"é",
		"e",
		"zeta",
		"ä",
		"",
	}
	refs := []string{
		"018f6b8e-0000-7000-8000-000000000001",
		"f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"00000000-0000-4000-8000-00000000000a",
		"a0000000-0000-4000-8000-000000000000",
	}
	base := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	rows := make([]Row, 200)
	for i := range rows {
		name := pick(texts...)
		row := Row{
			// Random ids, so the tiebreak order is unrelated to insertion order.
			ID:      fmt.Sprintf("%08x-%03d", rng.Uint32(), i),
			Tenant:  pick("t1", "t2"),
			Name:    name,
			NameCI:  name,
			Status:  pick(Labels...),
			Ref:     uuid.MustParse(pick(refs...)),
			Points:  []int64{-1, 0, 1, 2, math.MaxInt64, math.MinInt64}[rng.IntN(6)],
			Ratio:   []float32{0.1, -2.5, 3.25, 0}[rng.IntN(4)],
			Created: base.AddDate(0, 0, rng.IntN(3)).Add(time.Duration(rng.IntN(4)) * time.Microsecond),
		}
		if maybe(70) {
			note := pick(texts...)
			row.Note, row.NoteCI = &note, &note
		}
		if maybe(75) {
			status := pick(Labels...)
			row.StatusP = &status
		}
		if maybe(70) {
			points := int64(rng.IntN(3))
			row.PointsP = &points
		}
		if maybe(70) {
			score := []float64{-1.5, 0.1, 2.25, 1e300, -1e-300, 0}[rng.IntN(6)]
			row.Score = &score
		}
		if maybe(60) {
			seen := base.Add(time.Duration(rng.IntN(3)) * time.Hour)
			row.Seen = &seen
		}
		if maybe(80) {
			stamp := base.Add(time.Duration(rng.IntN(3))*time.Hour + time.Duration(rng.IntN(2))*time.Microsecond)
			row.Stamp = &stamp
		}
		rows[i] = row
	}
	return rows
}
