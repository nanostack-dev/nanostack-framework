package functional_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// The Tuple family is generated, so these tests cover the shape once at the
// low end, once in the middle, and once at the maximum arity — enough to catch
// a template that mis-numbers a field or an Unpack that returns the wrong one,
// without restating the same assertions eight times.

func TestTuple2(t *testing.T) {
	tp := functional.NewTuple2("a", 1)
	if tp.First != "a" || tp.Second != 1 {
		t.Fatalf("NewTuple2(\"a\", 1) = %+v, want {First:a Second:1}", tp)
	}

	first, second := tp.Unpack()
	if first != "a" || second != 1 {
		t.Fatalf("Unpack() = (%q, %d), want (\"a\", 1)", first, second)
	}
}

func TestTuple3(t *testing.T) {
	tp := functional.NewTuple3("a", 1, true)
	if tp.First != "a" || tp.Second != 1 || !tp.Third {
		t.Fatalf("NewTuple3(\"a\", 1, true) = %+v, want {First:a Second:1 Third:true}", tp)
	}

	first, second, third := tp.Unpack()
	if first != "a" || second != 1 || !third {
		t.Fatalf("Unpack() = (%q, %d, %t), want (\"a\", 1, true)", first, second, third)
	}
}

// TestTuple9 pins the maximum arity: every field must be distinct and Unpack
// must return them in declaration order, which is where an off-by-one in the
// generator would show up first.
func TestTuple9(t *testing.T) {
	tp := functional.NewTuple9(1, 2, 3, 4, 5, 6, 7, 8, 9)

	fields := []int{
		tp.First, tp.Second, tp.Third, tp.Fourth, tp.Fifth,
		tp.Sixth, tp.Seventh, tp.Eighth, tp.Ninth,
	}
	for i, got := range fields {
		if want := i + 1; got != want {
			t.Fatalf("field %d = %d, want %d", i+1, got, want)
		}
	}

	a, b, c, d, e, f, g, h, i := tp.Unpack()
	unpacked := []int{a, b, c, d, e, f, g, h, i}
	for idx, got := range unpacked {
		if want := idx + 1; got != want {
			t.Fatalf("Unpack()[%d] = %d, want %d", idx, got, want)
		}
	}
}

// TestTupleHoldsDistinctTypes pins the reason Tuple exists at all: the values
// it groups need not share a type.
func TestTupleHoldsDistinctTypes(t *testing.T) {
	type user struct{ Name string }

	tp := functional.NewTuple3(user{Name: "ada"}, 42, []string{"x"})
	if tp.First.Name != "ada" || tp.Second != 42 || len(tp.Third) != 1 {
		t.Fatalf("NewTuple3 with mixed types = %+v", tp)
	}
}
