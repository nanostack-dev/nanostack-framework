// Package apibind declares searches bound to the request bodies of
// openapi.yaml: the API mapper goldens and the build check read it.
// SearchWindows is served from the handler package, so the mapper of
// Window goes there.
package apibind

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/apibind/apigen"
)

func _() {
	nanorepo.Base[model.Items](table.Items)

	// API sort names (echopoint B10: route_name on sku), two names on one
	// key (anchor #7), NULLS LAST (anchor #22), InFold on a text column of enum-like values
	// (anchor #6), ContainsAny (anchor #11), an inherited full_text_search
	// that the search ignores.
	nanorepo.Search[model.Items]("Route", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Sku, nanorepo.InFold),
		nanorepo.Filter(table.Items.Name, nanorepo.ContainsAny),
		nanorepo.SortKey(table.Items.Sku).API("route_name"),
		nanorepo.SortKey(table.Items.Name).API("name", "resource"),
		nanorepo.SortKey(table.Items.Weight).NullsLast(),
		nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc),
		nanorepo.API[apigen.RouteSearchRequest](),
		nanorepo.IgnoresFullText("the product decides the columns"),
	)

	// Half-open (anchor #2) and exclusive (anchor #13) ranges; a
	// sort_direction with no sort_by (echopoint B7); a property that the
	// service resolves (echopoint B9). The handler is in another package.
	nanorepo.Search[model.Items]("Window", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("created_from", "created_to").HalfOpen()),
		nanorepo.Filter(table.Items.DeletedAt, nanorepo.Range("deleted_after", "deleted_before").Exclusive()),
		nanorepo.GoOnly(nanorepo.Filter(table.Items.ExternalID, nanorepo.Eq)),
		nanorepo.Sort(table.Items.CreatedAt),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
		nanorepo.API[apigen.WindowSearchRequest](),
		nanorepo.APIHandWritten("folder_id"),
	)

	// A one-sided range (echopoint B8: since only), a full text, a page.
	nanorepo.Search[model.Items]("Recent", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("since")),
		nanorepo.FullText(table.Items.Name),
		nanorepo.Sort(table.Items.CreatedAt),
		nanorepo.API[apigen.RecentSearchRequest](),
	)
}
