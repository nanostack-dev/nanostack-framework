// Package listx is a lint smoke fixture. It declares Go 1.27 generic methods
// before ordinary methods on the same type, the shape that Go 1.27.0 to 1.27.3
// export data reorders (golang/go#81188). Package consumer imports it, so
// linters read these methods from export data, not from source.
package listx

import "errors"

// ErrEmpty is returned by First on an empty list.
var ErrEmpty = errors.New("listx: empty list")

// List is an immutable slice with chainable methods.
type List[E any] []E

// Of returns a List holding items.
func Of[E any](items ...E) List[E] {
	return List[E](items)
}

// Map returns a new List with f applied to each element.
func (l List[E]) Map[F any](f func(E) F) List[F] {
	out := make(List[F], 0, len(l))
	for _, e := range l {
		out = append(out, f(e))
	}
	return out
}

// Fold reduces the list to one value, starting from initial.
func (l List[E]) Fold[A any](initial A, f func(A, E) A) A {
	acc := initial
	for _, e := range l {
		acc = f(acc, e)
	}
	return acc
}

// Filter returns the elements for which keep is true.
func (l List[E]) Filter(keep func(E) bool) List[E] {
	out := make(List[E], 0, len(l))
	for _, e := range l {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// First returns a pointer to the first element, or ErrEmpty.
func (l List[E]) First() (*E, error) {
	if len(l) == 0 {
		return nil, ErrEmpty
	}
	return &l[0], nil
}

// Validate returns an error when the list is empty.
func (l List[E]) Validate() error {
	if len(l) == 0 {
		return ErrEmpty
	}
	return nil
}

// Probe has the method shape of the reproducer in dominikh/go-tools#1746 on a
// non-generic receiver: a generic method first, then a method with one result
// that is always nil, then a method with three results. On Go 1.27.0 to 1.27.3
// export data moves Echo last, so a fact recorded for Nothing can be read back
// for Triple. golangci-lint v2.15.0 did not crash on it with SA4023 and
// nilness enabled; it stays as a guard.
type Probe struct{}

// Echo returns r unchanged.
func (Probe) Echo[R any](r R) R { return r }

// Nothing always returns nil.
func (Probe) Nothing() *int { return nil }

// Triple returns a zero count, a nil pointer and no error.
func (Probe) Triple() (int, *int, error) { return 0, nil, nil }
