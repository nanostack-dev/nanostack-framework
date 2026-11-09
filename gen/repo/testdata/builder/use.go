package builder

import (
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
)

// recentItems builds a search as a handler does: the forward stub must
// declare every name it uses before nanostack gen repo runs.
func recentItems(tenant uuid.UUID, since time.Time) ItemSearch {
	q := NewItemSearch(tenant)
	q = q.Status().In(model.ItemStatus_Active).CreatedAt().Since(since).Sku().In("a-1", sku("b-2"))
	q = q.LowerName().In("box").Expensive().Is(true).FullText("box")
	return q.SortBy(ItemSortWeight, search.SortDescending).Limit(10)
}

// ofTenant names the scope struct with keyed fields.
func ofTenant(tenant uuid.UUID, external string) ItemOfTenantSearch {
	return NewItemOfTenantSearch(ItemOfTenantScope{TenantID: tenant, ExternalID: external}).
		SortBy(ItemOfTenantSortPopularity, search.SortAscending)
}

func boards() BoardSearch { return NewBoardSearch().ID().Eq("b1") }

var _, _, _ = recentItems, ofTenant, boards
