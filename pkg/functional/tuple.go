package functional

// Tuple2 groups two values of possibly different types. Unlike Option and
// Result it carries no absence or failure state — it exists so a Map or
// FlatMap step can hand more than one value to whatever comes next without
// an ad hoc anonymous struct, e.g. pairing two lookups' values before a
// final combine.
type Tuple2[A, B any] struct {
	First  A
	Second B
}

// NewTuple2 builds a Tuple2 from its two values.
func NewTuple2[A, B any](a A, b B) Tuple2[A, B] {
	return Tuple2[A, B]{First: a, Second: b}
}

// Tuple3 groups three values of possibly different types.
type Tuple3[A, B, C any] struct {
	First  A
	Second B
	Third  C
}

// NewTuple3 builds a Tuple3 from its three values.
func NewTuple3[A, B, C any](a A, b B, c C) Tuple3[A, B, C] {
	return Tuple3[A, B, C]{First: a, Second: b, Third: c}
}
