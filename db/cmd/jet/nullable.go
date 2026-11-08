// This file is Nanostack-only (not in go-jet): the -nullable flag. See db/UPSTREAM.md.

package main

import (
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/template"
)

// parseNullable maps the -nullable flag to a generator strategy: pointer (*T, the go-jet
// default) or option (functional.Option[T]).
func parseNullable(s string) (template.Nullable, error) {
	switch s {
	case "pointer":
		return template.NullablePointer, nil
	case "option":
		return template.NullableOption, nil
	default:
		return nil, fmt.Errorf("-nullable=%q is not valid; use pointer (*T) or option (functional.Option[T])", s)
	}
}
