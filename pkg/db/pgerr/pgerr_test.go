package pgerr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
)

func TestIsUniqueViolation(t *testing.T) {
	const (
		constraint = "idx_platform_tenant_email"
		other      = "products_platform_tenant_id_name_key"
	)

	tests := []struct {
		name        string
		err         error
		constraints []string
		want        bool
	}{
		{
			name: "bare unique violation with no constraint filter",
			err:  &pq.Error{Code: pgerr.UniqueViolation},
			want: true,
		},
		{
			name:        "unique violation on the named constraint",
			err:         &pq.Error{Code: pgerr.UniqueViolation, Constraint: constraint},
			constraints: []string{constraint},
			want:        true,
		},
		{
			// The shape repositories actually see: go-jet wraps the driver error.
			name:        "jet-wrapped violation is unwrapped",
			err:         fmt.Errorf("jet: %w", &pq.Error{Code: pgerr.UniqueViolation, Constraint: constraint}),
			constraints: []string{constraint},
			want:        true,
		},
		{
			name: "violation nested several wraps deep",
			err: fmt.Errorf("create invitation: %w",
				fmt.Errorf("jet: %w", &pq.Error{Code: pgerr.UniqueViolation, Constraint: constraint})),
			constraints: []string{constraint},
			want:        true,
		},
		{
			name:        "any one of several constraints matches",
			err:         &pq.Error{Code: pgerr.UniqueViolation, Constraint: other},
			constraints: []string{constraint, other},
			want:        true,
		},
		{
			name:        "unique violation on a different constraint",
			err:         &pq.Error{Code: pgerr.UniqueViolation, Constraint: "platform_invitations_pkey"},
			constraints: []string{constraint},
			want:        false,
		},
		{
			name:        "different SQLSTATE on the named constraint",
			err:         &pq.Error{Code: "23503", Constraint: constraint},
			constraints: []string{constraint},
			want:        false,
		},
		{
			name: "foreign-key violation with no constraint filter",
			err:  &pq.Error{Code: "23503"},
			want: false,
		},
		{
			name: "non-pq error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name:        "non-pq error with a constraint filter",
			err:         errors.New("connection refused"),
			constraints: []string{constraint},
			want:        false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pgerr.IsUniqueViolation(tc.err, tc.constraints...); got != tc.want {
				t.Fatalf("IsUniqueViolation(%v, %v) = %v, want %v",
					tc.err, tc.constraints, got, tc.want)
			}
		})
	}
}
