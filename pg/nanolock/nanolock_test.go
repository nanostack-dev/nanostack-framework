package nanolock_test

import (
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"cirello.io/pglock"
	"github.com/XSAM/otelsql"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
)

// unreachable opens a pool on a closed port. No test below makes I/O on it:
// with AutoCreateTable and TestOnStartup off, NewClient only builds the
// client.
func unreachable(t *testing.T, open func(driver, dsn string) (*sql.DB, error)) *sql.DB {
	t.Helper()
	db, err := open("postgres", "host=127.0.0.1 port=1 sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func otelOpen(driver, dsn string) (*sql.DB, error) { return otelsql.Open(driver, dsn) }

func TestNewClientRejectsALeaseShorterThanTwoHeartbeats(t *testing.T) {
	db := unreachable(t, sql.Open)

	_, err := nanolock.NewClient(t.Context(), nanolock.Config{
		LeaseDuration: time.Second, HeartbeatFrequency: time.Second,
	}, db, slog.New(slog.DiscardHandler))

	if !errors.Is(err, pglock.ErrDurationTooSmall) {
		t.Fatalf("NewClient = %v, want pglock.ErrDurationTooSmall", err)
	}
	// pglock closes the pool it was given on this error; NewClient must
	// refuse before pglock sees the pool, which other modules share.
	if err = db.PingContext(t.Context()); errors.Is(err, sql.ErrConnDone) || isClosed(err) {
		t.Fatalf("the shared pool was closed: Ping = %v", err)
	}
}

func isClosed(err error) bool {
	return err != nil && err.Error() == "sql: database is closed"
}

func TestNewClientAcceptsThePoolOfNanopg(t *testing.T) {
	// nanopg.NewDB opens the pool with otelsql, whose driver is not
	// *pq.Driver. pglock.New refuses such a pool.
	db := unreachable(t, otelOpen)

	client, err := nanolock.NewClient(t.Context(), nanolock.Config{
		LeaseDuration: 10 * time.Second, HeartbeatFrequency: time.Second,
	}, db, slog.New(slog.DiscardHandler))

	if err != nil || client == nil {
		t.Fatalf("NewClient over an otelsql pool = (%v, %v), want a client", client, err)
	}
}

func TestNewClientDefaultsZeroDurations(t *testing.T) {
	// A zero lease would make every pglock call time out at once, because
	// pglock bounds each call with the lease.
	db := unreachable(t, otelOpen)

	client, err := nanolock.NewClient(t.Context(), nanolock.Config{}, db, slog.New(slog.DiscardHandler))
	if err != nil || client == nil {
		t.Fatalf("NewClient with zero durations = (%v, %v), want a client", client, err)
	}
}

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanolock.DefaultConfig())
}

// The defaults fill a zero field even when the other one is set: a lone 1s
// lease meets the 1s heartbeat default and is too short, and a lone 4s
// heartbeat fits the 10s lease default. A negative duration fails before
// pglock sees the pool. No case makes I/O.
func TestNewClientDurations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     nanolock.Config
		wantIs  error  // the error must wrap it
		wantMsg string // the error must contain it
	}{
		{"lease only, heartbeat default 1s", nanolock.Config{LeaseDuration: time.Second}, pglock.ErrDurationTooSmall, ""},
		{"heartbeat only, lease default 10s", nanolock.Config{HeartbeatFrequency: 4 * time.Second}, nil, ""},
		{"negative lease", nanolock.Config{LeaseDuration: -time.Second}, nil, "must not be negative"},
		{"negative heartbeat", nanolock.Config{HeartbeatFrequency: -time.Second}, nil, "must not be negative"},
		{"both negative", nanolock.Config{LeaseDuration: -time.Second, HeartbeatFrequency: -time.Second}, nil, "must not be negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := unreachable(t, otelOpen)

			client, err := nanolock.NewClient(t.Context(), tc.cfg, db, slog.New(slog.DiscardHandler))

			switch {
			case tc.wantIs != nil:
				if !errors.Is(err, tc.wantIs) {
					t.Fatalf("NewClient(%+v) = %v, want %v", tc.cfg, err, tc.wantIs)
				}
			case tc.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("NewClient(%+v) = %v, want an error containing %q", tc.cfg, err, tc.wantMsg)
				}
			case err != nil || client == nil:
				t.Fatalf("NewClient(%+v) = (%v, %v), want a client", tc.cfg, client, err)
			}
			if pingErr := db.PingContext(t.Context()); pingErr != nil && pingErr.Error() == "sql: database is closed" {
				t.Fatalf("the shared pool was closed: Ping = %v", pingErr)
			}
		})
	}
}
