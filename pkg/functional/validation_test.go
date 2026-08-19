package functional_test

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestValidation(t *testing.T) {
	t.Run("Valid carries the value and no errors", func(t *testing.T) {
		v := functional.Valid("value")
		if !v.IsValid() {
			t.Fatalf("IsValid() = false, want true")
		}
		if got := v.Value(); got != "value" {
			t.Fatalf("Value() = %q, want \"value\"", got)
		}
		if errs := v.Errors(); errs != nil {
			t.Fatalf("Errors() = %v, want nil", errs)
		}
		if err := v.Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})

	t.Run("Invalid accumulates every error in order", func(t *testing.T) {
		errName := errors.New("name is empty")
		errAge := errors.New("age is negative")
		v := functional.Invalid[string](errName, errAge)
		if v.IsValid() {
			t.Fatalf("IsValid() = true, want false")
		}
		errs := v.Errors()
		if len(errs) != 2 || !errors.Is(errs[0], errName) || !errors.Is(errs[1], errAge) {
			t.Fatalf("Errors() = %v, want [%v %v]", errs, errName, errAge)
		}
	})

	t.Run("Invalid with no errors still reports invalid", func(t *testing.T) {
		v := functional.Invalid[string]()
		if v.IsValid() {
			t.Fatalf("IsValid() = true, want false — Invalid must never claim validity")
		}
		if err := v.Err(); err == nil {
			t.Fatalf("Err() = nil, want a substituted error")
		}
		if got := len(v.Errors()); got != 1 {
			t.Fatalf("len(Errors()) = %d, want 1", got)
		}
	})

	t.Run("Invalid drops nil errors but keeps the verdict", func(t *testing.T) {
		errOnly := errors.New("only real error")
		v := functional.Invalid[string](nil, errOnly, nil)
		errs := v.Errors()
		if len(errs) != 1 || !errors.Is(errs[0], errOnly) {
			t.Fatalf("Errors() = %v, want [%v]", errs, errOnly)
		}

		allNil := functional.Invalid[string](nil, nil)
		if allNil.IsValid() {
			t.Fatalf("IsValid() = true, want false — all-nil input must not become valid")
		}
	})
}

func TestValidationErr(t *testing.T) {
	errFirst := errors.New("first reason")
	errSecond := errors.New("second reason")
	errThird := errors.New("third reason")

	t.Run("Err stays errors.Is-matchable for every accumulated error", func(t *testing.T) {
		joined := functional.Invalid[int](errFirst, errSecond, errThird).Err()
		for _, want := range []error{errFirst, errSecond, errThird} {
			if !errors.Is(joined, want) {
				t.Fatalf("errors.Is(Err(), %v) = false, want true", want)
			}
		}
	})

	t.Run("Err is nil when valid", func(t *testing.T) {
		if err := functional.Valid(1).Err(); err != nil {
			t.Fatalf("Err() = %v, want nil", err)
		}
	})
}

func TestValidationErrorsDefensiveCopy(t *testing.T) {
	errFirst := errors.New("first reason")
	errSecond := errors.New("second reason")
	v := functional.Invalid[int](errFirst, errSecond)

	stolen := v.Errors()
	stolen[0] = errors.New("mutated by caller")
	stolen[1] = nil

	fresh := v.Errors()
	if len(fresh) != 2 || !errors.Is(fresh[0], errFirst) || !errors.Is(fresh[1], errSecond) {
		t.Fatalf("Errors() after caller mutation = %v, want [%v %v]", fresh, errFirst, errSecond)
	}
	if !errors.Is(v.Err(), errFirst) || !errors.Is(v.Err(), errSecond) {
		t.Fatalf("Err() lost accumulated errors after caller mutated Errors() result")
	}
}

func TestValidationMap(t *testing.T) {
	double := func(n int) int { return n * 2 }

	t.Run("maps the value when valid", func(t *testing.T) {
		got := functional.Valid(21).Map(double)
		if !got.IsValid() {
			t.Fatalf("IsValid() = false, want true")
		}
		if v := got.Value(); v != 42 {
			t.Fatalf("Value() = %d, want 42", v)
		}
	})

	t.Run("carries every error through when invalid", func(t *testing.T) {
		errFirst := errors.New("first reason")
		errSecond := errors.New("second reason")
		got := functional.Invalid[int](errFirst, errSecond).Map(double)
		if got.IsValid() {
			t.Fatalf("IsValid() = true, want false")
		}
		errs := got.Errors()
		if len(errs) != 2 || !errors.Is(errs[0], errFirst) || !errors.Is(errs[1], errSecond) {
			t.Fatalf("Errors() = %v, want [%v %v]", errs, errFirst, errSecond)
		}
	})
}

func TestValidationMapErrors(t *testing.T) {
	t.Run("transforms the errors when invalid", func(t *testing.T) {
		errRaw := errors.New("raw reason")
		got := functional.Invalid[int](errRaw).MapErrors(func(errs []error) []error {
			wrapped := make([]error, len(errs))
			for i, err := range errs {
				wrapped[i] = errors.Join(errors.New("field name"), err)
			}
			return wrapped
		})
		if got.IsValid() {
			t.Fatalf("IsValid() = true, want false")
		}
		if !errors.Is(got.Err(), errRaw) {
			t.Fatalf("Err() = %v, want a wrap of %v", got.Err(), errRaw)
		}
	})

	t.Run("leaves a valid Validation untouched", func(t *testing.T) {
		called := false
		got := functional.Valid(1).MapErrors(func(errs []error) []error {
			called = true
			return errs
		})
		if called {
			t.Fatalf("f was called on a valid Validation, want not called")
		}
		if !got.IsValid() || got.Value() != 1 {
			t.Fatalf("got invalid or wrong value, want Valid(1)")
		}
	})

	t.Run("cannot erase the verdict by returning no errors", func(t *testing.T) {
		got := functional.Invalid[int](errors.New("reason")).MapErrors(
			func([]error) []error { return nil },
		)
		if got.IsValid() {
			t.Fatalf("IsValid() = true, want false — MapErrors must not flip invalid to valid")
		}
	})
}

func TestValidationBridges(t *testing.T) {
	errFirst := errors.New("first reason")
	errSecond := errors.New("second reason")

	t.Run("ToResult succeeds when valid", func(t *testing.T) {
		v, err := functional.Valid("value").ToResult().Value()
		if v != "value" || err != nil {
			t.Fatalf("ToResult().Value() = (%q, %v), want (\"value\", nil)", v, err)
		}
	})

	t.Run("ToResult joins every error when invalid", func(t *testing.T) {
		got := functional.Invalid[string](errFirst, errSecond).ToResult()
		if got.IsOk() {
			t.Fatalf("IsOk() = true, want false")
		}
		if !errors.Is(got.Err(), errFirst) || !errors.Is(got.Err(), errSecond) {
			t.Fatalf("Err() = %v, want a join of both errors", got.Err())
		}
	})

	t.Run("ToOption is present when valid", func(t *testing.T) {
		got := functional.Valid("value").ToOption()
		if v, ok := got.Get(); !ok || v != "value" {
			t.Fatalf("Get() = (%q, %t), want (\"value\", true)", v, ok)
		}
	})

	t.Run("ToOption is a failure, never absence, when invalid", func(t *testing.T) {
		got := functional.Invalid[string](errFirst, errSecond).ToOption()
		if got.IsPresent() {
			t.Fatalf("IsPresent() = true, want false")
		}
		if !errors.Is(got.Err(), errFirst) || !errors.Is(got.Err(), errSecond) {
			t.Fatalf("Err() = %v, want a join of both errors", got.Err())
		}
	})
}

func TestZipValidation2(t *testing.T) {
	errFirst := errors.New("first reason")
	errSecond := errors.New("second reason")

	t.Run("both valid yields a valid tuple", func(t *testing.T) {
		got := functional.ZipValidation2(functional.Valid("a"), functional.Valid(1))
		if !got.IsValid() {
			t.Fatalf("IsValid() = false, want true")
		}
		first, second := got.Value().Unpack()
		if first != "a" || second != 1 {
			t.Fatalf("Value().Unpack() = (%q, %d), want (\"a\", 1)", first, second)
		}
	})

	t.Run("one invalid input carries its errors", func(t *testing.T) {
		got := functional.ZipValidation2(functional.Valid("a"), functional.Invalid[int](errSecond))
		if got.IsValid() {
			t.Fatalf("IsValid() = true, want false")
		}
		errs := got.Errors()
		if len(errs) != 1 || !errors.Is(errs[0], errSecond) {
			t.Fatalf("Errors() = %v, want [%v]", errs, errSecond)
		}
	})

	t.Run("both invalid accumulates both, unlike ZipResult", func(t *testing.T) {
		got := functional.ZipValidation2(
			functional.Invalid[string](errFirst),
			functional.Invalid[int](errSecond),
		)
		errs := got.Errors()
		if len(errs) != 2 || !errors.Is(errs[0], errFirst) || !errors.Is(errs[1], errSecond) {
			t.Fatalf("Errors() = %v, want [%v %v]", errs, errFirst, errSecond)
		}
	})
}

func TestZipValidation3AccumulatesLeftToRight(t *testing.T) {
	errNameEmpty := errors.New("name is empty")
	errNameTooLong := errors.New("name is too long")
	errAge := errors.New("age is negative")
	errEmail := errors.New("email is malformed")

	t.Run("three invalid inputs report every error in argument order", func(t *testing.T) {
		got := functional.ZipValidation3(
			functional.Invalid[string](errNameEmpty, errNameTooLong),
			functional.Invalid[int](errAge),
			functional.Invalid[string](errEmail),
		)
		errs := got.Errors()
		want := []error{errNameEmpty, errNameTooLong, errAge, errEmail}
		if len(errs) != len(want) {
			t.Fatalf("len(Errors()) = %d, want %d", len(errs), len(want))
		}
		for i, wantErr := range want {
			if !errors.Is(errs[i], wantErr) {
				t.Fatalf("Errors()[%d] = %v, want %v", i, errs[i], wantErr)
			}
		}
	})

	t.Run("a valid input in the middle contributes nothing to the order", func(t *testing.T) {
		got := functional.ZipValidation3(
			functional.Invalid[string](errNameEmpty),
			functional.Valid(30),
			functional.Invalid[string](errEmail),
		)
		errs := got.Errors()
		if len(errs) != 2 || !errors.Is(errs[0], errNameEmpty) || !errors.Is(errs[1], errEmail) {
			t.Fatalf("Errors() = %v, want [%v %v]", errs, errNameEmpty, errEmail)
		}
	})

	t.Run("Err on the zipped result matches each accumulated error", func(t *testing.T) {
		joined := functional.ZipValidation3(
			functional.Invalid[string](errNameEmpty),
			functional.Invalid[int](errAge),
			functional.Invalid[string](errEmail),
		).Err()
		for _, want := range []error{errNameEmpty, errAge, errEmail} {
			if !errors.Is(joined, want) {
				t.Fatalf("errors.Is(Err(), %v) = false, want true", want)
			}
		}
	})
}

func TestZipValidation9(t *testing.T) {
	t.Run("all nine valid wires values through in order", func(t *testing.T) {
		got := functional.ZipValidation9(
			functional.Valid(1), functional.Valid(2), functional.Valid(3),
			functional.Valid(4), functional.Valid(5), functional.Valid(6),
			functional.Valid(7), functional.Valid(8), functional.Valid(9),
		)
		if !got.IsValid() {
			t.Fatalf("IsValid() = false, want true")
		}
		tuple := got.Value()
		if tuple.First != 1 || tuple.Fifth != 5 || tuple.Ninth != 9 {
			t.Fatalf(
				"tuple = %+v, want First 1, Fifth 5, Ninth 9",
				tuple,
			)
		}
	})

	t.Run("scattered invalid inputs accumulate left to right", func(t *testing.T) {
		errSecond := errors.New("second reason")
		errFifth := errors.New("fifth reason")
		errNinth := errors.New("ninth reason")
		got := functional.ZipValidation9(
			functional.Valid(1), functional.Invalid[int](errSecond), functional.Valid(3),
			functional.Valid(4), functional.Invalid[int](errFifth), functional.Valid(6),
			functional.Valid(7), functional.Valid(8), functional.Invalid[int](errNinth),
		)
		errs := got.Errors()
		want := []error{errSecond, errFifth, errNinth}
		if len(errs) != len(want) {
			t.Fatalf("len(Errors()) = %d, want %d", len(errs), len(want))
		}
		for i, wantErr := range want {
			if !errors.Is(errs[i], wantErr) {
				t.Fatalf("Errors()[%d] = %v, want %v", i, errs[i], wantErr)
			}
		}
	})
}
