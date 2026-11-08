package nanolock_test

import (
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"cirello.io/pglock"

	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
)

func TestNewClientRejectsALeaseShorterThanTwoHeartbeats(t *testing.T) {
	// pglock validates the durations before it touches the database.
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = nanolock.NewClient(t.Context(), nanolock.Config{
		LeaseDuration: time.Second, HeartbeatFrequency: time.Second,
	}, db, slog.New(slog.DiscardHandler))

	if !errors.Is(err, pglock.ErrDurationTooSmall) {
		t.Fatalf("NewClient = %v, want pglock.ErrDurationTooSmall", err)
	}
}
