package slicex

// Map transforms each item in source while preserving nil slices.
func Map[T, U any](source []T, transform func(T) U) []U {
	if source == nil {
		return nil
	}

	result := make([]U, len(source))
	for i, item := range source {
		result[i] = transform(item)
	}

	return result
}

// Filter returns items from source that match keep while preserving nil slices.
func Filter[T any](source []T, keep func(T) bool) []T {
	if source == nil {
		return nil
	}

	result := make([]T, 0, len(source))
	for _, item := range source {
		if keep(item) {
			result = append(result, item)
		}
	}

	return result
}

// FlatMap transforms each item into zero or more items while preserving nil slices.
func FlatMap[T, U any](source []T, transform func(T) []U) []U {
	if source == nil {
		return nil
	}

	result := make([]U, 0, len(source))
	for _, item := range source {
		result = append(result, transform(item)...)
	}

	return result
}

// Unique returns source values without duplicates, preserving first-seen order and nil slices.
func Unique[T comparable](source []T) []T {
	if source == nil {
		return nil
	}

	result := make([]T, 0, len(source))
	seen := make(map[T]struct{}, len(source))
	for _, item := range source {
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}

	return result
}

// Diff returns values from a that are not present in b while preserving order and nil slices.
func Diff[T comparable](a, b []T) []T {
	if a == nil {
		return nil
	}

	diff := make([]T, 0)
	setB := make(map[T]struct{}, len(b))
	for _, item := range b {
		setB[item] = struct{}{}
	}
	for _, item := range a {
		if _, found := setB[item]; !found {
			diff = append(diff, item)
		}
	}
	return diff
}

// ToMap indexes source by key. Later items replace earlier items with the same key.
func ToMap[T any, K comparable](source []T, key func(T) K) map[K]T {
	result := make(map[K]T, len(source))
	for _, item := range source {
		result[key(item)] = item
	}
	return result
}

// ContainsBy reports whether any item in source matches predicate.
func ContainsBy[T any](source []T, predicate func(T) bool) bool {
	for _, item := range source {
		if predicate(item) {
			return true
		}
	}
	return false
}

// StringDiff returns values from a that are not present in b.
func StringDiff(a, b []string) []string {
	return Diff(a, b)
}
