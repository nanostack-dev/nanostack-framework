package transactor_test

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

var (
	errDomain      = errors.New("domain: already exists")
	errOtherDomain = errors.New("domain: parent missing")
)

const (
	emailIndex = "idx_platform_tenant_email"
	pkeyIndex  = "platform_invitations_pkey"
	fkTenant   = "products_platform_tenant_id_fkey"
)

// resultWith builds a Result carrying err, exercising the same construction the
// query helpers use. The value is preserved so tests can assert it survives
// translation.
func resultWith(err error) transactor.Result[string] {
	return transactor.NewResultForTest("value", err)
}

func TestResultValueAndErr(t *testing.T) {
	t.Run("success carries the value and a nil error", func(t *testing.T) {
		v, err := resultWith(nil).Value()
		if v != "value" || err != nil {
			t.Fatalf("Value() = (%q, %v), want (\"value\", nil)", v, err)
		}
		if got := resultWith(nil).Err(); got != nil {
			t.Fatalf("Err() = %v, want nil", got)
		}
	})

	t.Run("failure carries the error", func(t *testing.T) {
		sentinel := errors.New("boom")
		if _, err := resultWith(sentinel).Value(); !errors.Is(err, sentinel) {
			t.Fatalf("Value() err = %v, want %v", err, sentinel)
		}
		if got := resultWith(sentinel).Err(); !errors.Is(got, sentinel) {
			t.Fatalf("Err() = %v, want %v", got, sentinel)
		}
	})
}

func TestResultOnUnique(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		constraints []string
		want        error
	}{
		{
			name:        "translates a violation on the named constraint",
			err:         &pq.Error{Code: pgerr.UniqueViolation, Constraint: emailIndex},
			constraints: []string{emailIndex},
			want:        errDomain,
		},
		{
			name:        "translates through go-jet wrapping",
			err:         fmt.Errorf("jet: %w", &pq.Error{Code: pgerr.UniqueViolation, Constraint: emailIndex}),
			constraints: []string{emailIndex},
			want:        errDomain,
		},
		{
			name:        "leaves a violation on a different constraint alone",
			err:         &pq.Error{Code: pgerr.UniqueViolation, Constraint: pkeyIndex},
			constraints: []string{emailIndex},
			want:        nil, // sentinel: expect the original error back
		},
		{
			name:        "leaves a foreign-key violation alone",
			err:         &pq.Error{Code: pgerr.ForeignKeyViolation, Constraint: emailIndex},
			constraints: []string{emailIndex},
			want:        nil,
		},
		{
			name:        "leaves a non-driver error alone",
			err:         errors.New("connection refused"),
			constraints: []string{emailIndex},
			want:        nil,
		},
		{
			// Documented sharp edge: an un-narrowed rule also absorbs a
			// primary-key collision, which is a bug rather than a conflict.
			name: "un-narrowed rule absorbs any unique violation, including the primary key",
			err:  &pq.Error{Code: pgerr.UniqueViolation, Constraint: pkeyIndex},
			want: errDomain,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resultWith(tc.err).OnUnique(errDomain, tc.constraints...).Err()
			want := tc.want
			if want == nil {
				want = tc.err // untranslated: the original error must survive
			}
			if !errors.Is(got, want) {
				t.Fatalf("OnUnique(...).Err() = %v, want %v", got, want)
			}
		})
	}
}

func TestResultOnForeignKey(t *testing.T) {
	t.Run("translates a violation on the named constraint", func(t *testing.T) {
		err := &pq.Error{Code: pgerr.ForeignKeyViolation, Constraint: fkTenant}
		got := resultWith(err).OnForeignKey(errOtherDomain, fkTenant).Err()
		if !errors.Is(got, errOtherDomain) {
			t.Fatalf("OnForeignKey(...).Err() = %v, want %v", got, errOtherDomain)
		}
	})

	t.Run("leaves a unique violation alone", func(t *testing.T) {
		err := &pq.Error{Code: pgerr.UniqueViolation, Constraint: fkTenant}
		got := resultWith(err).OnForeignKey(errOtherDomain, fkTenant).Err()
		if !errors.Is(got, err) {
			t.Fatalf("OnForeignKey(...).Err() = %v, want the original error", got)
		}
	})
}

func TestResultOnSQLState(t *testing.T) {
	t.Run("matches an arbitrary SQLSTATE", func(t *testing.T) {
		err := &pq.Error{Code: "23514", Constraint: "products_name_check"}
		got := resultWith(err).OnSQLState("23514", errDomain, "products_name_check").Err()
		if !errors.Is(got, errDomain) {
			t.Fatalf("OnSQLState(...).Err() = %v, want %v", got, errDomain)
		}
	})

	t.Run("ignores a different SQLSTATE", func(t *testing.T) {
		err := &pq.Error{Code: "23514"}
		got := resultWith(err).OnSQLState("23505", errDomain).Err()
		if !errors.Is(got, err) {
			t.Fatalf("OnSQLState(...).Err() = %v, want the original error", got)
		}
	})
}

func TestResultOnNoRows(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // nil sentinel: expect the original error back
	}{
		{name: "translates sql.ErrNoRows", err: sql.ErrNoRows, want: errDomain},
		{name: "translates qrm.ErrNoRows", err: qrm.ErrNoRows, want: errDomain},
		{name: "translates through wrapping", err: fmt.Errorf("jet: %w", qrm.ErrNoRows), want: errDomain},
		{name: "leaves a unique violation alone", err: &pq.Error{Code: pgerr.UniqueViolation}, want: nil},
		{name: "leaves a non-driver error alone", err: errors.New("connection refused"), want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resultWith(tc.err).OnNoRows(errDomain).Err()
			want := tc.want
			if want == nil {
				want = tc.err
			}
			if !errors.Is(got, want) {
				t.Fatalf("OnNoRows(...).Err() = %v, want %v", got, want)
			}
		})
	}

	t.Run("is a no-op on success", func(t *testing.T) {
		v, err := resultWith(nil).OnNoRows(errDomain).Value()
		if err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
		if v != "value" {
			t.Fatalf("Value() = %q, want the value preserved", v)
		}
	})
}

// TestResultChaining pins the first-match-wins contract: once a rule replaces
// the driver error, later rules see a domain error and cannot match.
func TestResultChaining(t *testing.T) {
	t.Run("first matching rule wins", func(t *testing.T) {
		err := &pq.Error{Code: pgerr.UniqueViolation, Constraint: emailIndex}
		got := resultWith(err).
			OnUnique(errDomain, emailIndex).
			OnUnique(errOtherDomain). // un-narrowed, but the error is no longer a pq error
			Err()
		if !errors.Is(got, errDomain) {
			t.Fatalf("chained Err() = %v, want %v", got, errDomain)
		}
	})

	t.Run("non-matching rules fall through to a later match", func(t *testing.T) {
		err := &pq.Error{Code: pgerr.ForeignKeyViolation, Constraint: fkTenant}
		got := resultWith(err).
			OnUnique(errDomain, emailIndex).
			OnForeignKey(errOtherDomain, fkTenant).
			Err()
		if !errors.Is(got, errOtherDomain) {
			t.Fatalf("chained Err() = %v, want %v", got, errOtherDomain)
		}
	})

	t.Run("rules are no-ops on success", func(t *testing.T) {
		v, err := resultWith(nil).
			OnUnique(errDomain).
			OnForeignKey(errOtherDomain).
			Value()
		if err != nil {
			t.Fatalf("Err() = %v, want nil — rules must not fire on success", err)
		}
		if v != "value" {
			t.Fatalf("Value() = %q, want the value preserved through the chain", v)
		}
	})

	t.Run("translation preserves the value", func(t *testing.T) {
		err := &pq.Error{Code: pgerr.UniqueViolation, Constraint: emailIndex}
		if v, _ := resultWith(err).OnUnique(errDomain, emailIndex).Value(); v != "value" {
			t.Fatalf("Value() = %q, want the value preserved", v)
		}
	})
}
