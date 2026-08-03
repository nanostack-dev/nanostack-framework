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

func TestIsQueryCanceled(t *testing.T) {
	const (
		userRequest      = "canceling statement due to user request"
		statementTimeout = "canceling statement due to statement timeout"
	)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "statement canceled at the client's request",
			err:  &pq.Error{Code: pgerr.QueryCanceled, Message: userRequest},
			want: true,
		},
		{
			// The shape repositories actually see: go-jet wraps the driver error.
			name: "jet-wrapped cancellation is unwrapped",
			err:  fmt.Errorf("jet: %w", &pq.Error{Code: pgerr.QueryCanceled, Message: userRequest}),
			want: true,
		},
		{
			name: "cancellation nested several wraps deep",
			err: fmt.Errorf("count tenants: %w",
				fmt.Errorf("jet: %w", &pq.Error{Code: pgerr.QueryCanceled, Message: userRequest})),
			want: true,
		},
		{
			// statement_timeout shares the SQLSTATE but is a server-side fault:
			// matching it here would hide a slow query behind a benign severity.
			name: "statement timeout shares the SQLSTATE",
			err:  &pq.Error{Code: pgerr.QueryCanceled, Message: statementTimeout},
			want: false,
		},
		{
			name: "different SQLSTATE carrying the same message",
			err:  &pq.Error{Code: pgerr.UniqueViolation, Message: userRequest},
			want: false,
		},
		{
			// No string fallback: an error that only quotes the message — a pool
			// fault echoing a prior failure — is not itself a cancellation.
			name: "non-pq error quoting the message",
			err:  errors.New("pq: " + userRequest),
			want: false,
		},
		{
			name: "unrelated pq error",
			err:  &pq.Error{Code: "3D000", Message: "database does not exist"},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pgerr.IsQueryCanceled(tc.err); got != tc.want {
				t.Fatalf("IsQueryCanceled(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
