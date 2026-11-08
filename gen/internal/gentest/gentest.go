// Package gentest holds test helpers for the generator contract of package
// gen.
package gentest

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Problems returns the problems in err and fails the test when err is nil,
// when a problem is not a *gen.Error the generator built, or when one has no
// Fix: every generator error names its fix.
func Problems(t testing.TB, err error) []*gen.Error {
	t.Helper()
	if err == nil {
		t.Fatal("got no error")
	}
	problems := gen.Errors(err)
	for _, p := range problems {
		if p.Fix == "" {
			t.Errorf("problem %q has no Fix (cause %T); build it with gen.Errorf", p.Msg, p.Err)
		}
		if p.Msg == "" {
			t.Errorf("problem with fix %q has no Msg", p.Fix)
		}
	}
	return problems
}

// Environment reports whether any problem in err wraps gen.ErrEnvironment.
func Environment(err error) bool {
	for _, p := range gen.Errors(err) {
		if errors.Is(p, gen.ErrEnvironment) {
			return true
		}
	}
	return false
}
