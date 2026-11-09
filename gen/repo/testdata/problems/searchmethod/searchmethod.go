// Package searchmethod binds repositories whose methods take declared
// searches, and breaks each rule of the search branch of the resolution
// (design G8): E18, E19, E21, E22, E23 and E24.
package searchmethod

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

type Item struct{ ID string }

func toItem(row model.Items) Item { return Item{ID: row.ID} }

type itemMapper struct{}

func (itemMapper) toItem(row model.Items) Item { return Item{ID: row.ID} }

// searcher runs any search; repositoryImpl embeds it, so its method is
// promoted.
type searcher struct{}

func (searcher) SearchPromoted(context.Context, PromotedSearch) (search.Result[Item], error) {
	return search.Result[Item]{}, nil
}

type Repository interface {
	SearchFine(ctx context.Context, q FineSearch) (search.Result[Item], error)
	SearchHandSort(ctx context.Context, q HandSortSearch) (search.Result[Item], error)
	SearchBackground(ctx context.Context, q BackgroundSearch) (search.Result[Item], error)
	SearchReassigned(ctx context.Context, q ReassignedSearch) (search.Result[Item], error)
	SearchForeignSpec(ctx context.Context, q ForeignSpecSearch) (search.Result[Item], error)
	SearchIgnored(ctx context.Context, q IgnoredSearch) (search.Result[Item], error)
	SearchPromoted(ctx context.Context, q PromotedSearch) (search.Result[Item], error)
	SearchNoMapperField(ctx context.Context, q NoMapperFieldSearch) (search.Result[Item], error)
	SearchWrongResult(ctx context.Context, q WrongResultSearch) ([]Item, error)
}

type repositoryImpl struct {
	itemsBase
	searcher
}

// NoBase runs a generated search with no base embedded.
type NoBase interface {
	SearchNoBase(ctx context.Context, q NoBaseSearch) (search.Result[Item], error)
}

type noBaseImpl struct{ db qrm.DB }

func (r *repositoryImpl) SearchBackground(_ context.Context, q BackgroundSearch) (search.Result[Item], error) {
	return r.search(context.Background(), backgroundSearch, q.Query, toItem)
}

func (r *repositoryImpl) SearchReassigned(ctx context.Context, q ReassignedSearch) (search.Result[Item], error) {
	q = NewReassignedSearch(uuid.Nil)
	return r.search(ctx, reassignedSearch, q.Query, toItem)
}

func (r *repositoryImpl) SearchForeignSpec(ctx context.Context, q ForeignSpecSearch) (search.Result[Item], error) {
	return searchexec.Run(ctx, r.db, fineSearch, NewFineSearch(q.Scope()).Query, toItem)
}

func (r *repositoryImpl) SearchIgnored(context.Context, IgnoredSearch) (search.Result[Item], error) {
	return search.Result[Item]{}, nil
}

func _() {
	nanorepo.Implement[Repository, repositoryImpl](table.Items, toItem)
	nanorepo.Implement[NoBase, noBaseImpl](table.Items, toItem)

	nanorepo.Search[model.Items]("Fine", table.Items, nanorepo.Map(toItem), nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("HandSort", table.Items, nanorepo.Map(toItem), nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name), nanorepo.SortHandWritten("popularity"))
	nanorepo.Search[model.Items]("Background", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("Reassigned", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("ForeignSpec", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("Ignored", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("Promoted", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("NoMapperField", table.Items, nanorepo.Map((itemMapper).toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("WrongResult", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("NoBase", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
	nanorepo.Search[model.Items]("Unused", table.Items, nanorepo.Map(toItem),
		nanorepo.Scope(table.Items.TenantID), nanorepo.Sort(table.Items.Name))
}
