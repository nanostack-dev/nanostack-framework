// This file is Nanostack-only (not in go-jet): the schema, the data and the
// helpers of the db/keyset suites. See db/UPSTREAM.md.

package postgres

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

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
