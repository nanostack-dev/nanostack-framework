package functional_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestTuple2(t *testing.T) {
	tp := functional.NewTuple2("a", 1)
	if tp.First != "a" || tp.Second != 1 {
		t.Fatalf("NewTuple2(\"a\", 1) = %+v, want {First:a Second:1}", tp)
	}
}

func TestTuple3(t *testing.T) {
	tp := functional.NewTuple3("a", 1, true)
	if tp.First != "a" || tp.Second != 1 || tp.Third != true {
		t.Fatalf("NewTuple3(\"a\", 1, true) = %+v, want {First:a Second:1 Third:true}", tp)
	}
}
