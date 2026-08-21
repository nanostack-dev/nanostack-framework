package functional_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

type person struct {
	ID   string
	Name string
	Age  int
}

func people() []person {
	return []person{
		{ID: "1", Name: "ada", Age: 36},
		{ID: "2", Name: "bob", Age: 24},
		{ID: "3", Name: "cli", Age: 36},
	}
}

func equal[T comparable](t *testing.T, got, want []T) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %v, want %v (got %v)", i, got[i], want[i], got)
		}
	}
}

func TestSliceMap(t *testing.T) {
	t.Run("maps every item", func(t *testing.T) {
		got := functional.Slice([]int{1, 2, 3}).Map(strconv.Itoa)
		equal(t, got, []string{"1", "2", "3"})
	})

	t.Run("nil maps to nil", func(t *testing.T) {
		var source []int
		if got := functional.Slice(source).Map(strconv.Itoa); got != nil {
			t.Fatalf("Map() = %v, want nil", got)
		}
	})

	t.Run("empty maps to empty", func(t *testing.T) {
		got := functional.Slice([]int{}).Map(strconv.Itoa)
		if got == nil {
			t.Fatal("Map() = nil, want empty")
		}
		equal(t, got, []string{})
	})

	t.Run("chains into a different type and back", func(t *testing.T) {
		got := functional.Slice(people()).
			Map(func(p person) string { return p.Name }).
			Map(strings.ToUpper)
		equal(t, got, []string{"ADA", "BOB", "CLI"})
	})
}

func TestSliceMapIndexed(t *testing.T) {
	got := functional.Slice([]string{"a", "b"}).MapIndexed(func(i int, s string) string {
		return strconv.Itoa(i) + s
	})
	equal(t, got, []string{"0a", "1b"})

	var source []string
	if mapped := functional.Slice(source).MapIndexed(func(int, string) string { return "" }); mapped != nil {
		t.Fatalf("MapIndexed() = %v, want nil", mapped)
	}
}

func TestSliceFlatMap(t *testing.T) {
	t.Run("concatenates results", func(t *testing.T) {
		got := functional.Slice([]int{1, 2}).FlatMap(func(i int) []int { return []int{i, i * 10} })
		equal(t, got, []int{1, 10, 2, 20})
	})

	t.Run("nil maps to nil", func(t *testing.T) {
		var source []int
		if got := functional.Slice(source).FlatMap(func(int) []int { return nil }); got != nil {
			t.Fatalf("FlatMap() = %v, want nil", got)
		}
	})
}

func TestSliceMapResult(t *testing.T) {
	t.Run("collects every value", func(t *testing.T) {
		got := functional.Slice([]string{"1", "2"}).MapResult(func(s string) functional.Result[int] {
			return functional.New(strconv.Atoi(s))
		})
		values, err := got.Value()
		if err != nil {
			t.Fatalf("Value() error = %v, want nil", err)
		}
		equal(t, values, []int{1, 2})
	})

	t.Run("fails on the first error", func(t *testing.T) {
		calls := 0
		got := functional.Slice([]string{"1", "nope", "also-nope"}).MapResult(func(s string) functional.Result[int] {
			calls++
			return functional.New(strconv.Atoi(s))
		})
		if got.IsOk() {
			t.Fatal("IsOk() = true, want false")
		}
		if calls != 2 {
			t.Fatalf("calls = %d, want 2", calls)
		}
	})

	t.Run("nil is ok and nil", func(t *testing.T) {
		var source []string
		got := functional.Slice(source).MapResult(func(string) functional.Result[int] { return functional.Ok(0) })
		values, err := got.Value()
		if err != nil {
			t.Fatalf("Value() error = %v, want nil", err)
		}
		if values != nil {
			t.Fatalf("Value() = %v, want nil", values)
		}
	})
}

func TestSliceMapValidation(t *testing.T) {
	t.Run("accumulates every error", func(t *testing.T) {
		got := functional.Slice([]string{"1", "a", "b"}).MapValidation(func(s string) functional.Validation[int] {
			value, err := strconv.Atoi(s)
			if err != nil {
				return functional.Invalid[int](err)
			}
			return functional.Valid(value)
		})
		if got.IsValid() {
			t.Fatal("IsValid() = true, want false")
		}
		if len(got.Errors()) != 2 {
			t.Fatalf("Errors() = %v, want 2 errors", got.Errors())
		}
	})

	t.Run("collects every value", func(t *testing.T) {
		got := functional.Slice([]string{"1", "2"}).MapValidation(func(s string) functional.Validation[int] {
			value, _ := strconv.Atoi(s)
			return functional.Valid(value)
		})
		if !got.IsValid() {
			t.Fatalf("IsValid() = false, want true (%v)", got.Errors())
		}
		equal(t, got.Value(), []int{1, 2})
	})
}

func TestSliceFilter(t *testing.T) {
	isEven := func(i int) bool { return i%2 == 0 }

	t.Run("keeps matching items", func(t *testing.T) {
		equal(t, functional.Slice([]int{1, 2, 3, 4}).Filter(isEven), []int{2, 4})
	})

	t.Run("FilterNot drops matching items", func(t *testing.T) {
		equal(t, functional.Slice([]int{1, 2, 3, 4}).FilterNot(isEven), []int{1, 3})
	})

	t.Run("nil filters to nil", func(t *testing.T) {
		var source []int
		if got := functional.Slice(source).Filter(isEven); got != nil {
			t.Fatalf("Filter() = %v, want nil", got)
		}
	})
}

func TestSliceFilterMap(t *testing.T) {
	isEven := func(i int) bool { return i%2 == 0 }

	t.Run("filters then maps in one pass", func(t *testing.T) {
		got := functional.Slice([]int{1, 2, 3, 4}).FilterMap(isEven, strconv.Itoa)
		equal(t, got, []string{"2", "4"})
	})

	t.Run("matches the unfused chain", func(t *testing.T) {
		source := []int{1, 2, 3, 4, 5, 6}
		fused := functional.Slice(source).FilterMap(isEven, strconv.Itoa)
		chained := functional.Slice(source).Filter(isEven).Map(strconv.Itoa)
		equal(t, fused, chained)
	})

	t.Run("applies f only to the items that match", func(t *testing.T) {
		calls := 0
		functional.Slice([]int{1, 2, 3, 4}).FilterMap(isEven, func(i int) int {
			calls++
			return i
		})
		if calls != 2 {
			t.Fatalf("calls = %d, want 2", calls)
		}
	})

	t.Run("nil maps to nil", func(t *testing.T) {
		var source []int
		if got := functional.Slice(source).FilterMap(isEven, strconv.Itoa); got != nil {
			t.Fatalf("FilterMap() = %v, want nil", got)
		}
	})
}

func TestSlicePeekAndForEach(t *testing.T) {
	seen := make([]int, 0)
	got := functional.Slice([]int{1, 2}).
		Peek(func(i int) { seen = append(seen, i) }).
		Map(func(i int) int { return i * 2 })
	equal(t, seen, []int{1, 2})
	equal(t, got, []int{2, 4})

	total := 0
	functional.Slice([]int{1, 2, 3}).ForEach(func(i int) { total += i })
	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
}

func TestSliceTakeDropReverseConcat(t *testing.T) {
	source := []int{1, 2, 3}

	equal(t, functional.Slice(source).Take(2), []int{1, 2})
	equal(t, functional.Slice(source).Take(0), []int{})
	equal(t, functional.Slice(source).Take(-1), []int{})
	equal(t, functional.Slice(source).Take(99), []int{1, 2, 3})
	equal(t, functional.Slice(source).Drop(2), []int{3})
	equal(t, functional.Slice(source).Drop(99), []int{})
	equal(t, functional.Slice(source).Reverse(), []int{3, 2, 1})
	equal(t, functional.Slice(source).Concat([]int{4}), []int{1, 2, 3, 4})
	equal(t, source, []int{1, 2, 3})

	var nilSource []int
	if got := functional.Slice(nilSource).Take(1); got != nil {
		t.Fatalf("Take() = %v, want nil", got)
	}
	if got := functional.Slice(nilSource).Concat(nil); got != nil {
		t.Fatalf("Concat() = %v, want nil", got)
	}
}

func TestSliceUniqueByAndSorted(t *testing.T) {
	t.Run("UniqueBy keeps the first item per key", func(t *testing.T) {
		got := functional.Slice(people()).UniqueBy(func(p person) int { return p.Age })
		equal(t, got.Map(func(p person) string { return p.ID }), []string{"1", "2"})
	})

	t.Run("SortedBy leaves the source untouched", func(t *testing.T) {
		source := people()
		got := functional.Slice(source).SortedBy(func(p person) int { return p.Age })
		equal(t, got.Map(func(p person) string { return p.ID }), []string{"2", "1", "3"})
		equal(t, functional.Slice(source).Map(func(p person) string { return p.ID }), []string{"1", "2", "3"})
	})

	t.Run("SortedWith uses the comparator", func(t *testing.T) {
		got := functional.Slice([]int{3, 1, 2}).SortedWith(func(a, b int) int { return b - a })
		equal(t, got, []int{3, 2, 1})
	})
}

func TestSliceCounts(t *testing.T) {
	source := functional.Slice([]int{1, 2, 3, 4})

	if got := source.Count(); got != 4 {
		t.Fatalf("Count() = %d, want 4", got)
	}
	if got := source.CountBy(func(i int) bool { return i > 2 }); got != 2 {
		t.Fatalf("CountBy() = %d, want 2", got)
	}
	if source.IsEmpty() {
		t.Fatal("IsEmpty() = true, want false")
	}
	if !functional.Slice([]int{}).IsEmpty() {
		t.Fatal("IsEmpty() = false, want true")
	}
	if !source.AnyMatch(func(i int) bool { return i == 3 }) {
		t.Fatal("AnyMatch() = false, want true")
	}
	if !source.AllMatch(func(i int) bool { return i > 0 }) {
		t.Fatal("AllMatch() = false, want true")
	}
	if !source.NoneMatch(func(i int) bool { return i > 9 }) {
		t.Fatal("NoneMatch() = false, want true")
	}
	if !functional.Slice([]int{}).AllMatch(func(int) bool { return false }) {
		t.Fatal("AllMatch() on empty = false, want true")
	}
}

func TestSliceOptionTerminals(t *testing.T) {
	source := functional.Slice(people())

	if got := source.FindFirst(func(p person) bool { return p.Age == 36 }).Value().ID; got != "1" {
		t.Fatalf("FindFirst() = %q, want %q", got, "1")
	}
	if source.FindFirst(func(p person) bool { return p.Age == 99 }).IsPresent() {
		t.Fatal("FindFirst() is present, want absent")
	}
	if got := source.Head().Value().ID; got != "1" {
		t.Fatalf("Head() = %q, want %q", got, "1")
	}
	if got := source.Last().Value().ID; got != "3" {
		t.Fatalf("Last() = %q, want %q", got, "3")
	}
	if got := source.At(1).Value().ID; got != "2" {
		t.Fatalf("At(1) = %q, want %q", got, "2")
	}
	if source.At(9).IsPresent() {
		t.Fatal("At(9) is present, want absent")
	}

	empty := functional.Slice([]person{})
	if empty.Head().IsPresent() || empty.Last().IsPresent() {
		t.Fatal("Head()/Last() on empty are present, want absent")
	}

	if got := source.Map(func(p person) string { return p.Name }).
		FindFirst(func(name string) bool { return strings.HasPrefix(name, "b") }).
		OrElse("none"); got != "bob" {
		t.Fatalf("chained FindFirst() = %q, want %q", got, "bob")
	}
}

func TestSliceReduceAndFold(t *testing.T) {
	sum := functional.Slice([]int{1, 2, 3}).Reduce(func(acc, item int) int { return acc + item })
	if got := sum.OrElse(0); got != 6 {
		t.Fatalf("Reduce() = %d, want 6", got)
	}
	if functional.Slice([]int{}).Reduce(func(acc, _ int) int { return acc }).IsPresent() {
		t.Fatal("Reduce() on empty is present, want absent")
	}

	got := functional.Slice(people()).FoldLeft(0, func(acc int, p person) int { return acc + p.Age })
	if got != 96 {
		t.Fatalf("FoldLeft() = %d, want 96", got)
	}
}

func TestSlicePartitionGroupAndMaps(t *testing.T) {
	parts := functional.Slice([]int{1, 2, 3, 4}).Partition(func(i int) bool { return i%2 == 0 })
	equal(t, parts.First, []int{2, 4})
	equal(t, parts.Second, []int{1, 3})

	byID := functional.Slice(people()).ToMap(func(p person) string { return p.ID })
	if len(byID) != 3 || byID["2"].Name != "bob" {
		t.Fatalf("ToMap() = %v", byID)
	}

	byAge := functional.Slice(people()).GroupBy(func(p person) int { return p.Age })
	if len(byAge) != 2 || len(byAge[36]) != 2 {
		t.Fatalf("GroupBy() = %v", byAge)
	}
}

func TestSliceInterop(t *testing.T) {
	t.Run("a Seq is usable as a plain slice", func(t *testing.T) {
		var plain []string = functional.Slice([]int{1, 2}).Map(strconv.Itoa)
		equal(t, plain, []string{"1", "2"})
		equal(t, functional.Slice([]int{1}).Map(strconv.Itoa).ToSlice(), []string{"1"})
	})

	t.Run("SeqOf takes values", func(t *testing.T) {
		equal(t, functional.SeqOf(1, 2, 3), []int{1, 2, 3})
	})

	t.Run("JoinString renders and joins", func(t *testing.T) {
		got := functional.Slice(people()).JoinString(", ", func(p person) string { return p.Name })
		if got != "ada, bob, cli" {
			t.Fatalf("JoinString() = %q", got)
		}
	})
}

func TestSliceComparableHelpers(t *testing.T) {
	equal(t, functional.Distinct(functional.Slice([]string{"a", "b", "a"})), []string{"a", "b"})
	equal(t, functional.Sorted(functional.Slice([]int{3, 1, 2})), []int{1, 2, 3})

	if got := functional.Sum(functional.Slice([]int{1, 2, 3})); got != 6 {
		t.Fatalf("Sum() = %d, want 6", got)
	}
	if got := functional.Sum(functional.Slice([]string{"a", "b"})); got != "ab" {
		t.Fatalf("Sum() = %q, want %q", got, "ab")
	}
	if !functional.SeqContains(functional.Slice([]int{1, 2}), 2) {
		t.Fatal("SeqContains() = false, want true")
	}
	equal(t, functional.Diff(functional.Slice([]int{1, 2, 3}), []int{2}), []int{1, 3})

	var nilSource []int
	if got := functional.Diff(functional.Slice(nilSource), []int{1}); got != nil {
		t.Fatalf("Diff() = %v, want nil", got)
	}
}

func TestSliceKeepsErrorsFromResultChains(t *testing.T) {
	sentinel := errors.New("boom")
	got := functional.Slice([]int{1}).MapResult(func(int) functional.Result[int] {
		return functional.Failure[int](sentinel)
	})
	if !errors.Is(got.Err(), sentinel) {
		t.Fatalf("Err() = %v, want %v", got.Err(), sentinel)
	}
}
