package ptr

// Ptr returns a pointer to v.
//
// It has no //go:fix inline directive. The inline analyzer cannot infer the
// type parameter at a call site, so with govet's enable-all (lint/base.golangci.yml)
// the directive reports "cannot inline" at every caller in another package.
// lint/smoke/consumer calls Ptr, so the framework lint fails when it returns.
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
