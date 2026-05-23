package slicex

import (
	"reflect"
	"testing"
)

func TestMap(t *testing.T) {
	if got := Map[int, string](nil, func(v int) string { return "x" }); got != nil {
		t.Fatalf("expected nil slice, got %#v", got)
	}

	got := Map([]int{1, 2, 3}, func(v int) string { return string(rune('0' + v)) })
	want := []string{"1", "2", "3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestStringDiff(t *testing.T) {
	got := StringDiff([]string{"read", "write", "delete"}, []string{"write"})
	want := []string{"read", "delete"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestFilter(t *testing.T) {
	if got := Filter[int](nil, func(v int) bool { return v > 1 }); got != nil {
		t.Fatalf("expected nil slice, got %#v", got)
	}

	got := Filter([]int{1, 2, 3, 4}, func(v int) bool { return v%2 == 0 })
	want := []int{2, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestFlatMap(t *testing.T) {
	if got := FlatMap[int, string](nil, func(v int) []string { return []string{"x"} }); got != nil {
		t.Fatalf("expected nil slice, got %#v", got)
	}

	got := FlatMap([]int{1, 2}, func(v int) []string {
		return []string{string(rune('0' + v)), string(rune('a' + v))}
	})
	want := []string{"1", "b", "2", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestUnique(t *testing.T) {
	if got := Unique[string](nil); got != nil {
		t.Fatalf("expected nil slice, got %#v", got)
	}

	got := Unique([]string{"read", "write", "read", "delete", "write"})
	want := []string{"read", "write", "delete"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestDiff(t *testing.T) {
	if got := Diff[string](nil, []string{"write"}); got != nil {
		t.Fatalf("expected nil slice, got %#v", got)
	}

	got := Diff([]int{1, 2, 3, 2}, []int{2})
	want := []int{1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestToMap(t *testing.T) {
	type item struct {
		ID   string
		Name string
	}

	got := ToMap([]item{{ID: "1", Name: "old"}, {ID: "2", Name: "two"}, {ID: "1", Name: "new"}}, func(v item) string {
		return v.ID
	})
	want := map[string]item{"1": {ID: "1", Name: "new"}, "2": {ID: "2", Name: "two"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestContainsBy(t *testing.T) {
	if !ContainsBy([]string{"read", "write"}, func(v string) bool { return v == "write" }) {
		t.Fatal("expected matching item")
	}

	if ContainsBy([]string{"read", "write"}, func(v string) bool { return v == "delete" }) {
		t.Fatal("expected no matching item")
	}
}
