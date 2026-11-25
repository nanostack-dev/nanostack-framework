package nanomigrate

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/lib/pq"
)

func TestIsDeadlockVictim(t *testing.T) {
	deadlock := &pq.Error{Code: "40P01", Message: "deadlock detected"}
	cases := map[string]struct {
		err  error
		want bool
	}{
		"migration body aborted": {
			err: fmt.Errorf("nanomigrate: migrate: %w",
				database.Error{OrigErr: deadlock, Err: "migration failed: deadlock detected"}),
			want: true,
		},
		// golang-migrate's SetVersion: the body already committed.
		"version write aborted": {
			err:  &database.Error{OrigErr: deadlock, Query: []byte("INSERT")},
			want: false,
		},
		"version commit aborted": {
			err:  &database.Error{OrigErr: deadlock, Err: "transaction commit failed"},
			want: false,
		},
		"bare driver deadlock": {err: fmt.Errorf("wrap: %w", deadlock), want: false},
		"migration failed, other code": {
			err:  database.Error{OrigErr: &pq.Error{Code: "42P07"}, Err: "migration failed"},
			want: false,
		},
		"not a pg error":  {err: errors.New("boom"), want: false},
		"no error at all": {err: nil, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isDeadlockVictim(tc.err); got != tc.want {
				t.Fatalf("isDeadlockVictim(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// A file that commits part of its work cannot run again after an abort, so
// its migration is not retried.
func TestTransactionControl(t *testing.T) {
	cases := map[string]bool{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t (c);":            false,
		"ALTER TABLE t ADD COLUMN c int;\nUPDATE t SET c = 1;":           false,
		"DO $$\nBEGIN\n  PERFORM 1;\nEND $$;":                            false,
		"BEGIN;\nCREATE TABLE a (id int);\nCOMMIT;":                      true,
		"CREATE TABLE a (id int);\ncommit;\nCREATE INDEX CONCURRENTLY i": true,
		"START TRANSACTION;\nSELECT 1;":                                  true,
		"CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\nEND;\n$$":        true, // Loose on purpose: no retry.
	}
	for body, want := range cases {
		if got := transactionControl.MatchString(body); got != want {
			t.Errorf("transactionControl(%q) = %t, want %t", body, got, want)
		}
	}
}
