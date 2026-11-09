package keyset

import (
	"encoding/json"
	"fmt"
	"regexp"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// Key is one column of a keyset ordering over scanned rows of T: the column
// to order and compare on, and how to read that column's value from a row.
//
// Build keys with the typed constructors (StringKey, UUIDKey, InetKey, EnumKey,
// TimeKey, LocalTimeKey, IntKey, FloatKey, RealKey and their Nullable forms)
// so the cursor value and the SQL literal share one type. A constructor
// refuses a cursor value its column cannot hold with search.ErrInvalidCursor,
// before any query runs.
type Key[T any] struct {
	column   jet.Column
	nullable bool
	nulls    nullPlacement
	// value reads the row's value, nil for SQL NULL.
	value func(T) any
	// bound decodes a cursor value into the comparisons against it. A JSON
	// null decodes to a bound with isNull set.
	bound func(json.RawMessage) (keyBound, error)
}

// nullPlacement says where NULLs sort. The zero value follows PostgreSQL:
// last when ascending, first when descending.
type nullPlacement int

const (
	nullsDefault nullPlacement = iota
	nullsFirst
	nullsLast
)

// NullsFirst returns the key with NULLs sorted before every value, in both
// directions. Use it when the API promises a placement that must not flip
// with the direction. PostgreSQL needs a matching index (... NULLS FIRST) to
// serve it without a sort.
func (k Key[T]) NullsFirst() Key[T] {
	k.nulls = nullsFirst
	return k
}

// NullsLast returns the key with NULLs sorted after every value, in both
// directions, for example "unlicensed organizations last".
func (k Key[T]) NullsLast() Key[T] {
	k.nulls = nullsLast
	return k
}

// nullsAfter reports whether NULLs follow every value in direction.
func (k Key[T]) nullsAfter(direction search.SortDirection) bool {
	switch k.nulls {
	case nullsFirst:
		return false
	case nullsLast:
		return true
	case nullsDefault:
		return direction != search.SortDescending
	}
	return direction != search.SortDescending
}

// keyBound holds the comparisons of one column against one cursor value.
type keyBound struct {
	isNull  bool
	literal jet.Expression
	gt      jet.BoolExpression
	lt      jet.BoolExpression
	eq      jet.BoolExpression
}

// keyCodec converts a column value to and from its cursor form. encode runs
// when a cursor is issued, decode when one comes back. decode must refuse any
// value the column cannot hold, so a forged cursor fails as ErrInvalidCursor
// and not as a database error.
type keyCodec[V any] struct {
	encode func(V) any
	decode func(json.RawMessage) (V, error)
}

func jsonCodec[V any]() keyCodec[V] {
	return keyCodec[V]{
		encode: func(v V) any { return v },
		decode: func(raw json.RawMessage) (V, error) {
			var v V
			err := json.Unmarshal(raw, &v)
			return v, err
		},
	}
}

// compareFunc builds the comparisons of the column against one value.
type compareFunc[V any] func(V) (gt, lt, eq jet.BoolExpression)

func newKey[T, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	literal func(V) jet.Expression,
	compare compareFunc[V],
) Key[T] {
	return newCodecKey(column, nullable, value, jsonCodec[V](), literal, compare)
}

func newCodecKey[T, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	codec keyCodec[V],
	literal func(V) jet.Expression,
	compare compareFunc[V],
) Key[T] {
	return Key[T]{
		column:   column,
		nullable: nullable,
		value: func(row T) any {
			if v := value(row); v != nil {
				return codec.encode(*v)
			}
			return nil
		},
		bound: func(raw json.RawMessage) (keyBound, error) {
			if string(raw) == "null" {
				if !nullable {
					return keyBound{}, fmt.Errorf(
						"%w: null for non-null column %s",
						search.ErrInvalidCursor,
						column.Name(),
					)
				}
				return keyBound{isNull: true, eq: column.IS_NULL()}, nil
			}
			v, err := codec.decode(raw)
			if err != nil {
				return keyBound{}, fmt.Errorf("%w: value for column %s", search.ErrInvalidCursor, column.Name())
			}
			gt, lt, eq := compare(v)
			return keyBound{literal: literal(v), gt: gt, lt: lt, eq: eq}, nil
		},
	}
}

func ptrOf[T, V any](value func(T) V) func(T) *V {
	return func(row T) *V {
		v := value(row)
		return &v
	}
}

// identifier is the form of a name that a key writes into the SQL as raw
// text: an enum type name today, and the name of a computed key.
var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_.]*$`)

// mustIdentifier panics when name is not a plain lower-case identifier. The
// name comes from code, never from a request, so a mismatch is a programming
// error.
func mustIdentifier(kind, name string) {
	if !identifier.MatchString(name) {
		panic(fmt.Sprintf("keyset: %s %q is not an identifier (%s)", kind, name, identifier))
	}
}
