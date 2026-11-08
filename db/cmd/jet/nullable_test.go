// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/generator/template"
)

func TestParseNullable(t *testing.T) {
	in := template.Type{Name: "int32"}

	pointer, err := parseNullable("pointer")
	require.NoError(t, err)
	require.Equal(t, "*int32", pointer(in).Name)

	option, err := parseNullable("option")
	require.NoError(t, err)
	require.Equal(t, "functional.Option[int32]", option(in).Name)

	for _, bad := range []string{"", "Option", "ptr"} {
		_, badErr := parseNullable(bad)
		require.EqualError(
			t,
			badErr,
			`-nullable="`+bad+`" is not valid; use pointer (*T) or option (functional.Option[T])`,
		)
	}
}
