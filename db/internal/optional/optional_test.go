package optional_test

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/optional"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

type modelLike struct {
	ID   int64
	Name string
}

// Option is a look-alike in another package: it must not match.
type Option[T any] struct{ v T }

func (o Option[T]) IsPresent() bool { return true }
func (o Option[T]) Value() T        { return o.v }
func (o Option[T]) Get() (T, bool)  { return o.v, true }

func TestElem(t *testing.T) {
	cases := []struct {
		t    reflect.Type
		elem reflect.Type
	}{
		{reflect.TypeFor[functional.Option[int64]](), reflect.TypeFor[int64]()},
		{reflect.TypeFor[functional.Option[uuid.UUID]](), reflect.TypeFor[uuid.UUID]()},
		{reflect.TypeFor[functional.Option[modelLike]](), reflect.TypeFor[modelLike]()},
		{reflect.TypeFor[functional.Option[*int]](), reflect.TypeFor[*int]()},
	}
	for _, c := range cases {
		elem, ok := optional.Elem(c.t)
		require.True(t, ok, c.t.String())
		require.Equal(t, c.elem, elem)
		require.Equal(t, c.elem, optional.ElemOrSelf(c.t))
	}

	for _, typ := range []reflect.Type{
		reflect.TypeFor[Option[int64]](),
		reflect.TypeFor[sql.NullString](),
		reflect.TypeFor[*functional.Option[int64]](),
		reflect.TypeFor[int](),
		reflect.TypeFor[functional.Result[int]](),
	} {
		_, ok := optional.Elem(typ)
		require.False(t, ok, typ.String())
		require.Equal(t, typ, optional.ElemOrSelf(typ))
	}
}

func TestUnwrap(t *testing.T) {
	v, ok := optional.Unwrap(functional.Some(1))
	require.True(t, ok)
	require.Equal(t, 1, v)

	v, ok = optional.Unwrap(functional.None[string]())
	require.True(t, ok)
	require.Nil(t, v)

	v, ok = optional.Unwrap(functional.Some(functional.Some("x")))
	require.True(t, ok)
	require.Equal(t, "x", v)

	v, ok = optional.Unwrap(functional.Some(functional.None[string]()))
	require.True(t, ok)
	require.Nil(t, v)

	for _, in := range []any{nil, 1, "a", Option[int]{v: 3}, sql.NullString{String: "a", Valid: true}} {
		v, ok = optional.Unwrap(in)
		require.False(t, ok)
		require.Equal(t, in, v)
	}
}

func TestSetGet(t *testing.T) {
	var o functional.Option[modelLike]
	dst := reflect.ValueOf(&o).Elem()

	_, present := optional.Get(dst)
	require.False(t, present)

	require.NoError(t, optional.Set(dst, reflect.ValueOf(modelLike{ID: 7, Name: "n"})))
	got, present := optional.Get(dst)
	require.True(t, present)
	require.Equal(t, modelLike{ID: 7, Name: "n"}, got.Interface())
	require.Equal(t, functional.Some(modelLike{ID: 7, Name: "n"}), o)

	require.Panics(t, func() { _ = optional.Set(dst, reflect.ValueOf(1)) })
	require.Panics(t, func() { optional.Get(reflect.ValueOf(1)) })
}
