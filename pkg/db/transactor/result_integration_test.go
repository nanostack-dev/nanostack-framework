//go:build integration

package transactor_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/go-jet/jet/v2/postgres"
	_ "github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// These tests run against a real PostgreSQL instance, because the value of the
// translation layer rests on facts no unit test can confirm: that the driver
// reports the SQLSTATE and constraint name we match on, and that go-jet's error
// wrapping survives errors.As.
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_PASSWORD=itpass -e POSTGRES_DB=pgerrit postgres:16
//	PGERR_TEST_DSN="postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable" \
//		go test -tags=integration ./pkg/db/transactor/
const dsnEnv = "PGERR_TEST_DSN"

var (
	errInvitationExists = errors.New("invitation already exists")
	errTenantHasChild   = errors.New("tenant still has products")
)

// Both names are declared explicitly in the schema below: Postgres would
// otherwise generate the foreign-key name, and the unique index name is what
// the driver reports for uniqueness declared with CREATE UNIQUE INDEX.
const (
	uniqueIndexName  = "idx_it_tenant_email"
	fkConstraintName = "it_invitations_tenant_fkey"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", dsnEnv)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

// setupSchema builds a schema mirroring the anchor shape that motivated this
// package: a table with a separate unique index and an ON DELETE RESTRICT
// parent reference.
func setupSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`DROP TABLE IF EXISTS it_invitations, it_tenants CASCADE`,
		`CREATE TABLE it_tenants (id text PRIMARY KEY)`,
		`CREATE TABLE it_invitations (
			id text PRIMARY KEY,
			tenant_id text NOT NULL
				CONSTRAINT it_invitations_tenant_fkey REFERENCES it_tenants(id) ON DELETE RESTRICT,
			email text NOT NULL
		)`,
		`CREATE UNIQUE INDEX ` + uniqueIndexName + ` ON it_invitations(tenant_id, email)`,
		`INSERT INTO it_tenants (id) VALUES ('t1')`,
		`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_a', 't1', 'a@x')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS it_invitations, it_tenants CASCADE`)
	})
}

func TestIntegrationOnUnique(t *testing.T) {
	db := openDB(t)
	setupSchema(t, db)
	ctx := context.Background()

	t.Run("duplicate business key translates to the domain error", func(t *testing.T) {
		stmt := postgres.RawStatement(
			`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_b', 't1', 'a@x')`,
		)
		err := transactor.Exec(ctx, db, stmt).
			OnUnique(errInvitationExists, uniqueIndexName).
			Err()
		if !errors.Is(err, errInvitationExists) {
			t.Fatalf("Err() = %v, want %v", err, errInvitationExists)
		}
	})

	t.Run("primary-key collision is NOT absorbed by a narrowed rule", func(t *testing.T) {
		// Same id, different business key: a real bug, and it must stay loud.
		stmt := postgres.RawStatement(
			`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_a', 't1', 'different@x')`,
		)
		err := transactor.Exec(ctx, db, stmt).
			OnUnique(errInvitationExists, uniqueIndexName).
			Err()
		if errors.Is(err, errInvitationExists) {
			t.Fatal("primary-key collision was translated to the business conflict; " +
				"a narrowed rule must not absorb it")
		}
		if err == nil {
			t.Fatal("expected the primary-key violation to surface")
		}
	})

	t.Run("un-narrowed rule DOES absorb a primary-key collision", func(t *testing.T) {
		// Pins the documented sharp edge so it cannot change silently.
		stmt := postgres.RawStatement(
			`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_a', 't1', 'another@x')`,
		)
		err := transactor.Exec(ctx, db, stmt).OnUnique(errInvitationExists).Err()
		if !errors.Is(err, errInvitationExists) {
			t.Fatalf("Err() = %v, want the un-narrowed rule to absorb it", err)
		}
	})

	t.Run("successful insert is untouched by the rules", func(t *testing.T) {
		stmt := postgres.RawStatement(
			`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_ok', 't1', 'ok@x')`,
		)
		if err := transactor.Exec(ctx, db, stmt).
			OnUnique(errInvitationExists, uniqueIndexName).
			Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})
}

func TestIntegrationOnForeignKey(t *testing.T) {
	db := openDB(t)
	setupSchema(t, db)
	ctx := context.Background()

	t.Run("ON DELETE RESTRICT translates to the domain error", func(t *testing.T) {
		stmt := postgres.RawStatement(`DELETE FROM it_tenants WHERE id = 't1'`)
		err := transactor.Exec(ctx, db, stmt).
			OnForeignKey(errTenantHasChild, fkConstraintName).
			Err()
		if !errors.Is(err, errTenantHasChild) {
			t.Fatalf("Err() = %v, want %v", err, errTenantHasChild)
		}
	})

	t.Run("insert naming a missing parent translates too", func(t *testing.T) {
		stmt := postgres.RawStatement(
			`INSERT INTO it_invitations (id, tenant_id, email) VALUES ('inv_c', 'MISSING', 'c@x')`,
		)
		err := transactor.Exec(ctx, db, stmt).
			OnForeignKey(errTenantHasChild, fkConstraintName).
			Err()
		if !errors.Is(err, errTenantHasChild) {
			t.Fatalf("Err() = %v, want %v", err, errTenantHasChild)
		}
	})

	t.Run("a unique rule does not absorb a foreign-key violation", func(t *testing.T) {
		stmt := postgres.RawStatement(`DELETE FROM it_tenants WHERE id = 't1'`)
		err := transactor.Exec(ctx, db, stmt).
			OnUnique(errInvitationExists, uniqueIndexName).
			Err()
		if errors.Is(err, errInvitationExists) {
			t.Fatal("foreign-key violation was translated by a unique rule")
		}
		if err == nil {
			t.Fatal("expected the foreign-key violation to surface")
		}
	})
}

// TestIntegrationQueryPathWrapping covers the go-jet Query path rather than
// Exec, since qrm wraps driver errors as fmt.Errorf("jet: %w", err) and the
// translation must still unwrap them.
func TestIntegrationQueryPathWrapping(t *testing.T) {
	db := openDB(t)
	setupSchema(t, db)
	ctx := context.Background()

	type row struct{ ID string }
	stmt := postgres.RawStatement(
		`INSERT INTO it_invitations (id, tenant_id, email)
		 VALUES ('inv_d', 't1', 'a@x') RETURNING id`,
	)
	_, err := transactor.Query[row](ctx, db, stmt).
		OnUnique(errInvitationExists, uniqueIndexName).
		Value()
	if !errors.Is(err, errInvitationExists) {
		t.Fatalf("Value() err = %v, want %v — jet wrapping must unwrap", err, errInvitationExists)
	}
}
