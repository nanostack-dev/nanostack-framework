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
		"*example.com/a/store.Repo":           "AStoreRepo",
		"*example.com/b/store.Repo":           "BStoreRepo",
		"*example.com/billing.Service":        "Service",
	}
	if got := fieldsOf(forward); !maps.Equal(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	if got := fieldsOf(backward); !maps.Equal(got, want) {
		t.Fatalf("fields of the reversed graph = %v, want %v", got, want)
	}
}

// Packages of one name are told apart by the fewest last elements of their
// import paths, whatever the order of the graph, so no field falls back to a
// number that depends on the other packages.
func TestFieldNamesQualifySamePackageNamesByImportPath(t *testing.T) {
	newNodes := func() []*node {
		return []*node{
			{typ: namedPtr("example.com/billing/repository", "repository", "Repository")},
			{typ: namedPtr("example.com/invoice/repository", "repository", "Repository")},
			{typ: namedPtr("example.com/eu/billing/store", "store", "Ledger")},
			{typ: namedPtr("example.com/us/billing/store", "store", "Ledger")},
			{typ: namedPtr("example.com/orders/store", "store", "Ledger")},
			{typ: namedPtr("example.com/cache", "cache", "Ledger")},
			{typ: namedPtr("gopkg.in/yaml.v3", "yaml", "Node")},
			{typ: namedPtr("example.com/yaml", "yaml", "Node")},
			{typ: namedPtr("example.com/kv", "kv", "Client")},
			{typ: namedPtr("example.com/kv/v2", "kv", "Client")},
			{typ: namedPtr("example.com/a/api", "api", "Server")},
			{typ: namedPtr("example.com/b/api", "api", "Server")},
			{typ: namedPtr("example.com/3d/geo", "geo", "Mesh")},
			{typ: namedPtr("example.com/2d/geo", "geo", "Mesh")},
			{typ: namedPtr("example.com/lib", "lib", "Repo")},
			{typ: named("example.com/lib", "lib", "Repo")},
		}
	}
	want := map[string]string{
		"*example.com/billing/repository.Repository": "BillingRepositoryRepository",
		"*example.com/invoice/repository.Repository": "InvoiceRepositoryRepository",
		"*example.com/eu/billing/store.Ledger":       "EuBillingStoreLedger",
		"*example.com/us/billing/store.Ledger":       "UsBillingStoreLedger",
		"*example.com/orders/store.Ledger":           "OrdersStoreLedger",
		"*example.com/cache.Ledger":                  "CacheLedger",
		"*gopkg.in/yaml.v3.Node":                     "YamlV3Node",
		"*example.com/yaml.Node":                     "YamlNode",
		"*example.com/kv.Client":                     "KvClient",
		"*example.com/kv/v2.Client":                  "KvV2Client",
		"*example.com/a/api.Server":                  "AAPIServer",
		"*example.com/b/api.Server":                  "BAPIServer",
		"*example.com/3d/geo.Mesh":                   "Pkg3dGeoMesh",
		"*example.com/2d/geo.Mesh":                   "Pkg2dGeoMesh",
		// One package: the import path cannot tell them apart.
		"*example.com/lib.Repo": "LibRepo",
		"example.com/lib.Repo":  "LibRepo2",
	}
	for _, reverse := range []bool{false, true} {
		nodes := newNodes()
		if reverse {
			slices.Reverse(nodes)
		}
		assignFields(nodes)
		if got := fieldsOf(nodes); !maps.Equal(got, want) {
			t.Fatalf("reverse=%v: fields = %v, want %v", reverse, got, want)
		}
	}
}

func TestPathQualifiers(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		want  map[string]string
	}{
		{
			[]string{"a/x/store", "b/x/store", "c/store"},
			map[string]string{"a/x/store": "AXStore", "b/x/store": "BXStore", "c/store": "CStore"},
		},
		{
			// A path that ends another one is qualified by all of it.
			[]string{"x/store", "a/x/store"},
			map[string]string{"x/store": "XStore", "a/x/store": "AXStore"},
		},
		{
			[]string{"example.com/store", "example.com/store/v2", "example.com/old/store/v3"},
			map[string]string{"example.com/store": "Store", "example.com/store/v2": "StoreV2", "example.com/old/store/v3": "StoreV3"},
		},
		{
			[]string{"github.com/acme/store", "gitlab.com/acme/store"},
			map[string]string{"github.com/acme/store": "GithubComAcmeStore", "gitlab.com/acme/store": "GitlabComAcmeStore"},
		},
	} {
		if got := pathQualifiers(tc.paths); !maps.Equal(got, tc.want) {
			t.Errorf("pathQualifiers(%v) = %v, want %v", tc.paths, got, tc.want)
		}
	}
}
