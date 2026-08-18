package transactor_test

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

func TestOptional(t *testing.T) {
	t.Run("present carries the value", func(t *testing.T) {
		o := transactor.NewOptionalForTest("value", true, nil)
		if !o.IsPresent() {
			t.Fatalf("IsPresent() = false, want true")
		}
		if got := o.Value(); got != "value" {
			t.Fatalf("Value() = %q, want %q", got, "value")
		}
		if err := o.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})

	t.Run("absent carries the zero value and no error", func(t *testing.T) {
		o := transactor.NewOptionalForTest("", false, nil)
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if err := o.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
		if got := o.Value(); got != "" {
			t.Fatalf("Value() = %q, want the zero value", got)
		}
	})

	t.Run("a failure is not present even if the row looks set", func(t *testing.T) {
		sentinel := errors.New("boom")
		o := transactor.NewOptionalForTest("value", true, sentinel)
		if o.IsPresent() {
			t.Fatalf("IsPresent() = true, want false — Err is non-nil")
		}
		if !errors.Is(o.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", o.Err(), sentinel)
		}
	})
}
