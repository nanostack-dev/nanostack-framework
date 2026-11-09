// Package torture is the torture fixture of nanostack gen repo: strange
// tables, odd columns and every kind of key and scope, on the torture schema
// (migrations/). Its integration tests run every derived method on
// PostgreSQL, for tenant isolation first and for correctness second.
//
// The tenant columns are org_id, product_id and site_id (hard scopes) and
// workspace_id and region_id (context scopes), declared as a service declares
// them in Postgres.Scopes, Postgres.ContextScopes, Postgres.ScopeNames and
// Postgres.ScopeImplies. Only names that derive are declared here: one
// refusal fails the whole package, so the names that must not derive are the
// package under gen/repo/testdata/torture, with a golden of each refusal.
package torture

// Scopes are the tenant columns of the fixture.
var Scopes = []string{
	"org_id",
	"product_id",
	"site_id",
	"workspace_id",
	"region_id",
} //nolint:gochecknoglobals // fixture

// ContextScopes registers the function that scopes workspace_id and region_id.
var ContextScopes = map[string]string{ //nolint:gochecknoglobals // fixture
	"workspace_id": "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Filter",
	"region_id":    "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/regionscope.Filter",
}

// ScopeNames are the short names of the tenant columns.
var ScopeNames = map[string]string{ //nolint:gochecknoglobals // fixture
	"org_id": "Org", "product_id": "Product", "site_id": "Site", "workspace_id": "Workspace", "region_id": "Region",
}

// ContextStamps registers the function that gives a new row its workspace.
var ContextStamps = map[string]string{ //nolint:gochecknoglobals // fixture
	"workspace_id": "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Stamp",
}

// ContextPresents registers the function that reports whether a context
// carries a workspace: a derived delete or write fails closed without one.
// region_id has none, so a derived delete on a table with a region fails open
// on a context without a region (and warns at generation).
var ContextPresents = map[string]string{ //nolint:gochecknoglobals // fixture
	"workspace_id": "github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Present",
}

// DatabaseColumns are the columns the database writes.
var DatabaseColumns = []string{"created_at", "updated_at"} //nolint:gochecknoglobals // fixture

// ScopeImplies says that a site belongs to one product, which belongs to one
// org. Only the table bin holds that in its data: the others seed every
// product under both orgs, so no derived method of theirs relies on it.
var ScopeImplies = map[string]string{
	"site_id":    "product_id",
	"product_id": "org_id",
} //nolint:gochecknoglobals // fixture
