package wire

import (
	"go/types"
	"maps"
	"slices"
	"testing"
)

func named(path, pkg, name string) *types.Named {
	obj := types.NewTypeName(0, types.NewPackage(path, pkg), name, nil)
	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

func namedPtr(path, pkg, name string) types.Type { return types.NewPointer(named(path, pkg, name)) }

func fieldsOf(order []*node) map[string]string {
	out := map[string]string{}
	for _, n := range order {
		out[types.TypeString(n.typ, nil)] = n.field
	}
	return out
}

// The field names are the API of Overrides and Components: a reordered graph
// must not rename them, and colliding names read as Go identifiers.
func TestFieldNamesDoNotDependOnTheGraphOrder(t *testing.T) {
	newNodes := func() []*node {
		return []*node{
			{typ: namedPtr("example.com/pg/nanopg", "nanopg", "DB")},
			{typ: namedPtr("database/sql", "sql", "DB")},
			{typ: namedPtr("example.com/http/nanoserver", "nanoserver", "Server")},
			{typ: namedPtr("example.com/http/nanopprof", "nanopprof", "Server")},
			{typ: types.NewSlice(named("example.com/core/log", "log", "ContextAttrs"))},
			{typ: namedPtr("example.com/a/store", "store", "Repo")},
			{typ: namedPtr("example.com/b/store", "store", "Repo")},
			{typ: namedPtr("example.com/billing", "billing", "Service")},
		}
	}
	forward := newNodes()
	assignFields(forward)
	backward := newNodes()
	slices.Reverse(backward)
	assignFields(backward)

	want := map[string]string{
		"*example.com/pg/nanopg.DB":           "NanopgDB",
		"*database/sql.DB":                    "SQLDB",
		"*example.com/http/nanoserver.Server": "NanoserverServer",
		"*example.com/http/nanopprof.Server":  "NanopprofServer",
		"[]example.com/core/log.ContextAttrs": "ContextAttrsList",
		"*example.com/a/store.Repo":           "StoreRepo",
		"*example.com/b/store.Repo":           "StoreRepo2",
		"*example.com/billing.Service":        "Service",
	}
	if got := fieldsOf(forward); !maps.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	if got := fieldsOf(backward); !maps.Equal(got, want) {
		t.Fatalf("fields of the reversed graph = %v, want %v", got, want)
	}
}
