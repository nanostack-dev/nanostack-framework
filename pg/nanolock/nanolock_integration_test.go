//go:build integration

package nanolock_test

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"cirello.io/pglock"

	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
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
