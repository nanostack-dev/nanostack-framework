//go:build integration

package nanolock_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"cirello.io/pglock"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
)

// Only a real database shows that the table is created, that the startup test
// passes on it and that a held lock excludes a second holder. The server
// starts in a container (see pg/internal/pgtest):
//
//	go test -tags=integration ./pg/nanolock/

func TestNewClientCreatesTheTableAndLocks(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	cfg := nanolock.Config{
		TableName: "it_locks", AutoCreateTable: true, TestOnStartup: true,
		LeaseDuration: 10 * time.Second, HeartbeatFrequency: time.Second,
	}

	client, err := nanolock.NewClient(t.Context(), cfg, db, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	lock, err := client.AcquireContext(t.Context(), "job", pglock.FailIfLocked())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err = client.AcquireContext(
		t.Context(),
		"job",
		pglock.FailIfLocked(),
	); !errors.Is(
		err,
		pglock.ErrNotAcquired,
	) {
		t.Fatalf("second acquire = %v, want pglock.ErrNotAcquired", err)
	}
	if err = lock.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// A second client finds the table and passes the startup test again.
	if _, err = nanolock.NewClient(t.Context(), cfg, db, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("NewClient on an existing table: %v", err)
	}
}

// The pool of a service comes from nanopg.NewDB, which wraps lib/pq with
// otelsql. pgtest.Open uses a bare lib/pq pool, so this test builds the pool
// the way a manifest does.
func TestNewClientOverTheNanopgPool(t *testing.T) {
	client, err := nanolock.NewClient(
		t.Context(),
		nanolock.DefaultConfig(),
		nanopgPool(t),
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewClient over the nanopg pool: %v", err)
	}
	lock, err := client.AcquireContext(t.Context(), "job", pglock.FailIfLocked())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err = lock.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestNewClientWithoutTheTableFailsTheStartupTest(t *testing.T) {
	db := pgtest.Open(t, pgtest.Fresh(t))
	_, err := nanolock.NewClient(t.Context(), nanolock.Config{
		TableName: "missing_locks", TestOnStartup: true,
		LeaseDuration: 10 * time.Second, HeartbeatFrequency: time.Second,
	}, db, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NewClient passed the startup test without a lock table")
	}
}

// A Config without durations, as a hand-built one in a test or an Override
// often is, gets the default lease and heartbeat. pglock bounds every call
// with the lease, so with a zero lease the first acquire fails at once with
// context.DeadlineExceeded.
func TestNewClientWithZeroDurationsLocks(t *testing.T) {
	client, err := nanolock.NewClient(
		t.Context(),
		nanolock.Config{AutoCreateTable: true},
		nanopgPool(t),
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewClient with zero durations: %v", err)
	}

	lock, err := client.AcquireContext(t.Context(), "job", pglock.FailIfLocked())
	if err != nil {
		t.Fatalf("acquire with zero durations: %v", err)
	}
	if err = lock.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// nanopgPool returns the pool of a fresh database, opened as nanopg.NewDB
// opens it in a service: lib/pq wrapped by otelsql.
func nanopgPool(t *testing.T) *sql.DB {
	t.Helper()
	pg := pgtest.Fresh(t)
	db, err := nanopg.NewDB(t.Context(), nanopg.Config{
		Host: pg.Host, Port: pg.Port, User: pg.User, Password: secret.New(pg.Password), DB: pg.Name,
		SSLMode: "disable", MaxOpenConns: 5, MaxIdleConns: 5,
	}, slog.New(slog.DiscardHandler), noop.NewTracerProvider())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Stop(context.WithoutCancel(t.Context())) })
	return nanopg.SQL(db)
}
