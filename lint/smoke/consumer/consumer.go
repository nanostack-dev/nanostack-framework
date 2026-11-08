// Package consumer is a lint smoke fixture. It calls the generic and ordinary
// methods of listx.List from another package, so the linters load listx from
// export data. That importer side is where golang/go#81188 crashes analyzers.
// It also calls the generic pkg/ptr.Ptr, so a //go:fix inline directive on a
// generic function fails the lint: the inline analyzer reports "cannot inline"
// at every caller in another package.
package consumer

import (
	"errors"
	"strconv"

	"github.com/nanostack-dev/nanostack-framework/lint/smoke/listx"
	"github.com/nanostack-dev/nanostack-framework/pkg/ptr"
)

// Lengths returns the decimal length of each even number in values.
func Lengths(values ...int) listx.List[int] {
	return listx.Of(values...).
		Filter(func(v int) bool { return v%2 == 0 }).
		Map(strconv.Itoa).
		Map(func(s string) int { return len(s) })
}

// Sum adds the values.
func Sum(values ...int) int {
	return listx.Of(values...).Fold(0, func(acc, v int) int { return acc + v })
}

// FirstOrZero returns the first value, or zero for an empty list.
func FirstOrZero(values ...int) (int, error) {
	if err := listx.Of(values...).Validate(); err != nil {
		if errors.Is(err, listx.ErrEmpty) {
			return 0, nil
		}
		return 0, err
	}
	first, err := listx.Of(values...).First()
	if err != nil {
		return 0, err
	}
	if first == nil {
		return 0, nil
	}
	return *first, nil
}

// ProbeCount calls listx.Probe the way the go-tools#1746 reproducer does: the
// three-result method, whose error is checked, and the generic method.
func ProbeCount() (int, error) {
	var probe listx.Probe
	count, _, err := probe.Triple()
	if err != nil {
		return 0, err
	}
	return probe.Echo(count), nil
}

// Name returns a pointer to name through the generic ptr.Ptr, as a service
// does from another module.
func Name(name string) *string {
	return ptr.Ptr(name)
}
