package ptr

// Ptr returns a pointer to v.
//
//go:fix inline
func Ptr[T any](v T) *T {
	return new(v)
}

// DerefOr returns fallback when v is nil.
func DerefOr[T any](v *T, fallback T) T {
	if v == nil {
		return fallback
	}

	return *v
}
