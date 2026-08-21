package functional

import (
	"cmp"
	"slices"
	"strings"
)

// Seq is a slice with chainable operations. It is a plain []T underneath, so a
// Seq[T] can be passed to and returned from anything that expects []T.
type Seq[T any] []T

// Slice starts a chain over an existing slice.
func Slice[T any](source []T) Seq[T] {
	return source
}

// SeqOf starts a chain over the given values.
func SeqOf[T any](values ...T) Seq[T] {
	return values
}

// Map applies f to every item. A nil Seq maps to a nil Seq.
func (s Seq[T]) Map[R any](f func(T) R) Seq[R] {
	if s == nil {
		return nil
	}

	result := make(Seq[R], len(s))
	for i, item := range s {
		result[i] = f(item)
	}

	return result
}

// MapIndexed applies f to every item together with its index.
func (s Seq[T]) MapIndexed[R any](f func(int, T) R) Seq[R] {
	if s == nil {
		return nil
	}

	result := make(Seq[R], len(s))
	for i, item := range s {
		result[i] = f(i, item)
	}

	return result
}

// FlatMap applies f to every item and concatenates the results.
func (s Seq[T]) FlatMap[R any](f func(T) []R) Seq[R] {
	if s == nil {
		return nil
	}

	result := make(Seq[R], 0, len(s))
	for _, item := range s {
		result = append(result, f(item)...)
	}

	return result
}

// MapResult applies f to every item and fails on the first error.
func (s Seq[T]) MapResult[R any](f func(T) Result[R]) Result[Seq[R]] {
	if s == nil {
		return Ok[Seq[R]](nil)
	}

	result := make(Seq[R], len(s))
	for i, item := range s {
		value, err := f(item).Value()
		if err != nil {
			return Failure[Seq[R]](err)
		}
		result[i] = value
	}

	return Ok(result)
}

// MapValidation applies f to every item and accumulates the errors of every item that fails.
func (s Seq[T]) MapValidation[R any](f func(T) Validation[R]) Validation[Seq[R]] {
	if s == nil {
		return Valid[Seq[R]](nil)
	}

	result := make(Seq[R], 0, len(s))
	errs := make([]error, 0)
	for _, item := range s {
		validated := f(item)
		if !validated.IsValid() {
			errs = append(errs, validated.Errors()...)
			continue
		}
		result = append(result, validated.Value())
	}

	if len(errs) > 0 {
		return Invalid[Seq[R]](errs...)
	}

	return Valid(result)
}

// Filter keeps the items that match pred. A nil Seq filters to a nil Seq.
func (s Seq[T]) Filter(pred func(T) bool) Seq[T] {
	if s == nil {
		return nil
	}

	result := make(Seq[T], 0, len(s))
	for _, item := range s {
		if pred(item) {
			result = append(result, item)
		}
	}

	return result
}

// FilterNot keeps the items that do not match pred.
func (s Seq[T]) FilterNot(pred func(T) bool) Seq[T] {
	return s.Filter(func(item T) bool { return !pred(item) })
}

// Peek runs f on every item and returns the Seq unchanged.
func (s Seq[T]) Peek(f func(T)) Seq[T] {
	for _, item := range s {
		f(item)
	}

	return s
}

// ForEach runs f on every item.
func (s Seq[T]) ForEach(f func(T)) {
	for _, item := range s {
		f(item)
	}
}

// Take keeps at most the first n items.
func (s Seq[T]) Take(n int) Seq[T] {
	if s == nil {
		return nil
	}
	if n < 0 {
		n = 0
	}
	if n > len(s) {
		n = len(s)
	}

	return slices.Clone(s[:n])
}

// Drop discards the first n items.
func (s Seq[T]) Drop(n int) Seq[T] {
	if s == nil {
		return nil
	}
	if n < 0 {
		n = 0
	}
	if n > len(s) {
		n = len(s)
	}

	return slices.Clone(s[n:])
}

// Reverse returns the items in the opposite order.
func (s Seq[T]) Reverse() Seq[T] {
	if s == nil {
		return nil
	}

	result := slices.Clone(s)
	slices.Reverse(result)

	return result
}

// Concat appends other after the items of s.
func (s Seq[T]) Concat(other []T) Seq[T] {
	if s == nil && other == nil {
		return nil
	}

	result := make(Seq[T], 0, len(s)+len(other))
	result = append(result, s...)
	result = append(result, other...)

	return result
}

// UniqueBy keeps the first item for every distinct key.
func (s Seq[T]) UniqueBy[K comparable](key func(T) K) Seq[T] {
	if s == nil {
		return nil
	}

	seen := make(map[K]struct{}, len(s))
	result := make(Seq[T], 0, len(s))
	for _, item := range s {
		k := key(item)
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		result = append(result, item)
	}

	return result
}

// SortedBy orders the items by key. The order of equal keys does not change.
func (s Seq[T]) SortedBy[K cmp.Ordered](key func(T) K) Seq[T] {
	if s == nil {
		return nil
	}

	result := slices.Clone(s)
	slices.SortStableFunc(result, func(a, b T) int { return cmp.Compare(key(a), key(b)) })

	return result
}

// SortedWith orders the items with compare. The order of equal items does not change.
func (s Seq[T]) SortedWith(compare func(a, b T) int) Seq[T] {
	if s == nil {
		return nil
	}

	result := slices.Clone(s)
	slices.SortStableFunc(result, compare)

	return result
}

// Count returns the number of items.
func (s Seq[T]) Count() int {
	return len(s)
}

// CountBy returns the number of items that match pred.
func (s Seq[T]) CountBy(pred func(T) bool) int {
	count := 0
	for _, item := range s {
		if pred(item) {
			count++
		}
	}

	return count
}

// IsEmpty reports whether the Seq holds no items.
func (s Seq[T]) IsEmpty() bool {
	return len(s) == 0
}

// AnyMatch reports whether at least one item matches pred.
func (s Seq[T]) AnyMatch(pred func(T) bool) bool {
	for _, item := range s {
		if pred(item) {
			return true
		}
	}

	return false
}

// AllMatch reports whether every item matches pred.
func (s Seq[T]) AllMatch(pred func(T) bool) bool {
	for _, item := range s {
		if !pred(item) {
			return false
		}
	}

	return true
}

// NoneMatch reports whether no item matches pred.
func (s Seq[T]) NoneMatch(pred func(T) bool) bool {
	return !s.AnyMatch(pred)
}

// FindFirst returns the first item that matches pred.
func (s Seq[T]) FindFirst(pred func(T) bool) Option[T] {
	for _, item := range s {
		if pred(item) {
			return Some(item)
		}
	}

	return None[T]()
}

// Head returns the first item.
func (s Seq[T]) Head() Option[T] {
	if len(s) == 0 {
		return None[T]()
	}

	return Some(s[0])
}

// Last returns the final item.
func (s Seq[T]) Last() Option[T] {
	if len(s) == 0 {
		return None[T]()
	}

	return Some(s[len(s)-1])
}

// At returns the item at index i.
func (s Seq[T]) At(i int) Option[T] {
	if i < 0 || i >= len(s) {
		return None[T]()
	}

	return Some(s[i])
}

// Reduce combines the items with f. An empty Seq reduces to an absent Option.
func (s Seq[T]) Reduce(f func(acc, item T) T) Option[T] {
	if len(s) == 0 {
		return None[T]()
	}

	acc := s[0]
	for _, item := range s[1:] {
		acc = f(acc, item)
	}

	return Some(acc)
}

// FoldLeft combines the items with f, from initial.
func (s Seq[T]) FoldLeft[R any](initial R, f func(acc R, item T) R) R {
	acc := initial
	for _, item := range s {
		acc = f(acc, item)
	}

	return acc
}

// Partition splits the items into the ones that match pred and the ones that do not.
func (s Seq[T]) Partition(pred func(T) bool) Tuple2[Seq[T], Seq[T]] {
	matched := make(Seq[T], 0, len(s))
	rest := make(Seq[T], 0, len(s))
	for _, item := range s {
		if pred(item) {
			matched = append(matched, item)
			continue
		}
		rest = append(rest, item)
	}

	return NewTuple2(matched, rest)
}

// ToMap indexes the items by key. A later item replaces an earlier item with the same key.
func (s Seq[T]) ToMap[K comparable](key func(T) K) map[K]T {
	result := make(map[K]T, len(s))
	for _, item := range s {
		result[key(item)] = item
	}

	return result
}

// GroupBy collects the items into one Seq for every distinct key.
func (s Seq[T]) GroupBy[K comparable](key func(T) K) map[K]Seq[T] {
	result := make(map[K]Seq[T])
	for _, item := range s {
		k := key(item)
		result[k] = append(result[k], item)
	}

	return result
}

// ToSlice returns the items as a plain slice.
func (s Seq[T]) ToSlice() []T {
	return s
}

// JoinString renders every item with render and joins the results with sep.
func (s Seq[T]) JoinString(sep string, render func(T) string) string {
	parts := make([]string, len(s))
	for i, item := range s {
		parts[i] = render(item)
	}

	return strings.Join(parts, sep)
}

// Distinct keeps the first occurrence of every value.
func Distinct[T comparable](s Seq[T]) Seq[T] {
	return s.UniqueBy(func(item T) T { return item })
}

// Sorted orders the values from low to high.
func Sorted[T cmp.Ordered](s Seq[T]) Seq[T] {
	return s.SortedBy(func(item T) T { return item })
}

// Sum adds every value.
func Sum[T cmp.Ordered](s Seq[T]) T {
	var total T
	for _, item := range s {
		total += item
	}

	return total
}

// SeqContains reports whether the Seq holds v.
func SeqContains[T comparable](s Seq[T], v T) bool {
	return slices.Contains(s, v)
}

// Diff returns the values of s that b does not hold.
func Diff[T comparable](s Seq[T], b []T) Seq[T] {
	if s == nil {
		return nil
	}

	exclude := make(map[T]struct{}, len(b))
	for _, item := range b {
		exclude[item] = struct{}{}
	}

	result := make(Seq[T], 0, len(s))
	for _, item := range s {
		if _, found := exclude[item]; found {
			continue
		}
		result = append(result, item)
	}

	return result
}
