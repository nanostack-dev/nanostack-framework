//go:build integration

package transactor_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// The it_keyset fixture is shared by the keyset tests and the search tests.
// It has no key forwarders of the transactor, so the search tests keep it
// when the keyset tests go.

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

func setupKeysetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_keyset`,
		`DROP TYPE IF EXISTS it_keyset_status`,
		// Declaration order differs from text order on purpose.
		`CREATE TYPE it_keyset_status AS ENUM ('pending', 'active', 'archived')`,
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
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS it_keyset`)
		_, _ = db.Exec(`DROP TYPE IF EXISTS it_keyset_status`)
	})
}
