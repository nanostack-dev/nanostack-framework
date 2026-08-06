package transactor_test

import (
	"errors"
	"fmt"
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

func TestOptionalMap(t *testing.T) {
	double := func(n int) int { return n * 2 }

	t.Run("maps the value when present", func(t *testing.T) {
		got := transactor.NewOptionalForTest(21, true, nil).Map(double)
		if !got.IsPresent() {
			t.Fatalf("IsPresent() = false, want true")
		}
		if got.Value() != 42 {
			t.Fatalf("Value() = %d, want 42", got.Value())
		}
	})

	t.Run("leaves absence untouched", func(t *testing.T) {
		got := transactor.NewOptionalForTest(0, false, nil).Map(double)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false — mapping absence stays absent")
		}
		if err := got.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})

	t.Run("leaves a failure untouched", func(t *testing.T) {
		sentinel := errors.New("boom")
		got := transactor.NewOptionalForTest(21, true, sentinel).Map(double)
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if !errors.Is(got.Err(), sentinel) {
			t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
		}
	})

	t.Run("changes type, not just value", func(t *testing.T) {
		toLabel := func(n int) string { return fmt.Sprintf("n=%d", n) }
		got := transactor.NewOptionalForTest(7, true, nil).Map(toLabel)
		if got.Value() != "n=7" {
			t.Fatalf("Value() = %q, want %q", got.Value(), "n=7")
		}
	})
}
