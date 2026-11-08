package functional_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestOptionMarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"None is null", functional.None[int](), `null`},
		{"present zero is the value", functional.Some(0), `0`},
		{"string", functional.Some("a"), `"a"`},
		{"omitzero drops None", struct {
			A functional.Option[int] `json:"a,omitzero"`
			B functional.Option[int] `json:"b,omitzero"`
		}{B: functional.Some(0)}, `{"b":0}`},
		{"field without omitzero", struct {
			A functional.Option[string] `json:"a"`
		}{}, `{"a":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestOptionUnmarshalJSON(t *testing.T) {
	t.Run("null and padded null are None", func(t *testing.T) {
		for _, in := range []string{`null`, ` null `} {
			o := functional.Some(3)
			if err := o.UnmarshalJSON([]byte(in)); err != nil {
				t.Fatal(err)
			}
			if o.IsPresent() {
				t.Fatalf("%q: got %v, want None", in, o)
			}
		}
	})

	t.Run("struct fields", func(t *testing.T) {
		var dest struct {
			A functional.Option[int]    `json:"a"`
			B functional.Option[string] `json:"b"`
			C functional.Option[int]    `json:"c"`
		}
		if err := json.Unmarshal([]byte(`{"a":0,"b":null}`), &dest); err != nil {
			t.Fatal(err)
		}
		if v, ok := dest.A.Get(); !ok || v != 0 {
			t.Fatalf("A = (%v, %v), want (0, true)", v, ok)
		}
		if dest.B.IsPresent() || dest.C.IsPresent() {
			t.Fatalf("B = %v, C = %v, want None for null and for a missing key", dest.B, dest.C)
		}
	})

	t.Run("time round trip", func(t *testing.T) {
		in := functional.Some(time.Date(2024, 1, 2, 3, 4, 5, 6000, time.FixedZone("x", 3600)))
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out functional.Option[time.Time]
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		if !out.IsPresent() || !out.Value().Equal(in.Value()) {
			t.Fatalf("got %v, want %v", out, in)
		}
	})

	t.Run("invalid JSON leaves the receiver unchanged", func(t *testing.T) {
		o := functional.Some(1)
		if err := o.UnmarshalJSON([]byte(`"x"`)); err == nil {
			t.Fatal("want an error")
		}
		if v, ok := o.Get(); !ok || v != 1 {
			t.Fatalf("got (%v, %v), want the unchanged (1, true)", v, ok)
		}
	})
}
