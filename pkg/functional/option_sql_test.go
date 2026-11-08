package functional_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

var _ sql.Scanner = (*functional.Option[int64])(nil)

// pickyID is a Scanner whose Scan accepts only strings, like many UUID or decimal types:
// it rejects a value of its own type.
type pickyID struct{ s string }

func (p *pickyID) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return errors.New("pickyID: want string")
	}
	p.s = s
	return nil
}

func TestOptionScan(t *testing.T) {
	t.Run("nil is None", func(t *testing.T) {
		o := functional.Some[int64](5)
		if err := o.Scan(nil); err != nil {
			t.Fatal(err)
		}
		if o.IsPresent() {
			t.Fatalf("Scan(nil) = %v, want None", o)
		}
	})

	t.Run("same type is stored as is", func(t *testing.T) {
		var o functional.Option[int64]
		if err := o.Scan(int64(42)); err != nil {
			t.Fatal(err)
		}
		if v, ok := o.Get(); !ok || v != 42 {
			t.Fatalf("got (%v, %v), want (42, true)", v, ok)
		}
	})

	t.Run("int64 into int32 converts like database/sql", func(t *testing.T) {
		var o functional.Option[int32]
		if err := o.Scan(int64(7)); err != nil {
			t.Fatal(err)
		}
		if v, ok := o.Get(); !ok || v != 7 {
			t.Fatalf("got (%v, %v), want (7, true)", v, ok)
		}
	})

	t.Run("bytes into string", func(t *testing.T) {
		var o functional.Option[string]
		if err := o.Scan([]byte("abc")); err != nil {
			t.Fatal(err)
		}
		if v, ok := o.Get(); !ok || v != "abc" {
			t.Fatalf("got (%q, %v), want (abc, true)", v, ok)
		}
	})

	t.Run("bytes into bytes are copied", func(t *testing.T) {
		src := []byte("abc")
		var o functional.Option[[]byte]
		if err := o.Scan(src); err != nil {
			t.Fatal(err)
		}
		src[0] = 'X'
		if v, ok := o.Get(); !ok || string(v) != "abc" {
			t.Fatalf("got (%q, %v), want (abc, true): the driver's buffer leaked into the Option", v, ok)
		}
	})

	t.Run("a Scanner T that rejects its own type still takes a typed value", func(t *testing.T) {
		var o functional.Option[pickyID]
		if err := o.Scan(pickyID{s: "done"}); err != nil {
			t.Fatal(err)
		}
		if v, ok := o.Get(); !ok || v.s != "done" {
			t.Fatalf("got (%v, %v), want ({done}, true)", v, ok)
		}
		if err := o.Scan("raw"); err != nil {
			t.Fatal(err)
		}
		if v, _ := o.Get(); v.s != "raw" {
			t.Fatalf("got %v, want {raw} through pickyID.Scan", v)
		}
	})

	t.Run("an error leaves the receiver unchanged", func(t *testing.T) {
		o := functional.Some[int64](1)
		if err := o.Scan("x"); err == nil {
			t.Fatal("Scan(\"x\") into Option[int64]: want an error")
		}
		if v, ok := o.Get(); !ok || v != 1 {
			t.Fatalf("got (%v, %v), want the unchanged (1, true)", v, ok)
		}
	})
}
