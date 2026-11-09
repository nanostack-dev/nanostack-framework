// Package refused declares names that must not derive on the torture tables:
// each is a refusal that the golden want.txt pins, with its fix. A name that
// derives is in internal/torture; one refusal here must never become a
// generated method that reads or deletes across tenants.
package refused

import (
	"context"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/view"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// ProductID is a named type over uuid.UUID that says another column than org_id.
type ProductID uuid.UUID

// Users has one hard scope, org_id, and the keys user_pkey (id),
// (org_id, external_id) and (org_id, name, name_key). Its partial index on
// (org_id, handle) and its expression index on (org_id, lower(email)) are not keys.
type Users interface {
	// Single-row finds that no key proves.
	FindByOrgAndHandle(ctx context.Context, orgID uuid.UUID, handle string) (functional.Option[model.User], error)
	GetByOrgAndHandle(ctx context.Context, orgID uuid.UUID, handle string) (model.User, error)
	FindByOrgAndEmail(ctx context.Context, orgID uuid.UUID, email string) (functional.Option[model.User], error)
	FindByOrgAndName(ctx context.Context, orgID uuid.UUID, name string) (functional.Option[model.User], error)
	// The scope rule.
	FindByID(ctx context.Context, id uuid.UUID) (functional.Option[model.User], error)
	FindByNameAndOrg(ctx context.Context, name string, orgID uuid.UUID) ([]model.User, error)
	FindAll(ctx context.Context) ([]model.User, error)
	Count(ctx context.Context) (int64, error)
	DeleteByNameInternal(ctx context.Context, name string) (int64, error)
	DeleteByID(ctx context.Context, id uuid.UUID) error
	// Internal is a column here: the name is read as the column, and the scope rule applies.
	FindByInternal(ctx context.Context, internal bool) ([]model.User, error)
	// Names that are no column, or use an operator.
	FindByOrgAndid(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (functional.Option[model.User], error)
	FindByOrgIdAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNmae(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndMixedcase(ctx context.Context, orgID uuid.UUID, v string) ([]model.User, error)
	FindByOrgAndNameIgnoreCase(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNameLike(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNameGreaterThan(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNameIsNull(ctx context.Context, orgID uuid.UUID) ([]model.User, error)
	FindByOrgAndNameNot(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNameOrEmail(ctx context.Context, orgID uuid.UUID, name, email string) ([]model.User, error)
	FindByOrgAndNameOrderByNameDesc(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndNameAnd(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindBy(ctx context.Context) ([]model.User, error)
	ListByOrg(ctx context.Context, orgID uuid.UUID) ([]model.User, error)
	// Parameters.
	FindByOrgAndHandleOrName(ctx context.Context, orgID uuid.UUID) ([]model.User, error)
	CountByOrgAndName(ctx context.Context, orgID uuid.UUID) (int64, error)
	CountByOrgAndNameAndEmail(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	ExistsByOrgAndName(ctx context.Context, orgID uuid.UUID, name *string) (bool, error)
	ExistsByOrgAndEmail(ctx context.Context, orgID uuid.UUID, email int) (bool, error)
	ExistsByOrgAndHandle(ctx context.Context, orgID string, handle string) (bool, error)
	DeleteByOrgAndNameAndEmail(ctx context.Context, orgID uuid.UUID, email, name string) error
	DeleteByOrgAndInternal(orgID uuid.UUID, internal bool) error
	// Results.
	FindByOrg(ctx context.Context, orgID uuid.UUID) []model.User
	FindByOrgAndNameKey(ctx context.Context, orgID uuid.UUID, nameKey string) (int, error)
	CountByOrg(ctx context.Context, orgID uuid.UUID) (bool, error)
	ExistsByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	DeleteByOrgAndHandle(ctx context.Context, orgID uuid.UUID, handle string) (string, error)
	FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (*model.User, error)
}

// Orders has two hard scopes and the nullable context scope workspace_id.
// Its keys are order_pkey (id), (org_id, product_id, by) and
// (org_id, product_id, workspace_id, name).
type Orders interface {
	// The context scope is not named.
	FindByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) ([]model.Order, error)
	DeleteByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (int64, error)
	DeleteByOrgAndProductAndNameInternal(ctx context.Context, orgID, productID uuid.UUID, name string) (int64, error)
	// The context scope in its short form binds no parameter, so it does not prove a key that holds workspace_id.
	FindByOrgAndProductAndWorkspaceAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (functional.Option[model.Order], error)
	// The first word is a context scope while a hard scope exists.
	// A named type that says another column of the table, for a scope parameter.
	FindByOrgIDAndProductIDAndWorkspaceAndName(ctx context.Context, orgID ProductID, productID uuid.UUID, name string) ([]model.Order, error)
	// The org only: every product of it. The workspace in its field form is no scope that the
	// others can leave out; and a delete across products is refused as a read is.
	FindByOrgAndWorkspaceAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.Order, error)
	DeleteByOrgAndWorkspaceAndName(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	FindByWorkspaceIDAndName(ctx context.Context, workspaceID, name string) ([]model.Order, error)
	// IDAndName is one column.
	FindByOrgAndProductAndWorkspaceAndIDAndName(ctx context.Context, orgID, productID uuid.UUID, id uuid.UUID, name string) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndBrand(ctx context.Context, orgID, productID uuid.UUID, brand string) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndNameOrderByNameDesc(ctx context.Context, orgID, productID uuid.UUID, name string) ([]model.Order, error)
	// Operators and Or with a context scope in the name: the hand-written body keeps it.
	FindByOrgAndProductAndWorkspaceAndNameIgnoreCase(ctx context.Context, orgID, productID uuid.UUID, name string) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndNameOrBy(ctx context.Context, orgID, productID uuid.UUID, name, by string) ([]model.Order, error)
	DeleteByWorkspaceAndNameInternal(ctx context.Context, name string) (int64, error)
	// A key without the order of the scopes.
	FindByNameAndOrgAndProductAndWorkspace(ctx context.Context, name string, orgID, productID uuid.UUID) ([]model.Order, error)
	FindByProductAndWorkspaceAndBy(ctx context.Context, productID uuid.UUID, by string) (functional.Option[model.Order], error)
}

// Ledgers has three hard scopes and a NOT NULL context scope, all in the
// primary key (org_id, product_id, site_id, workspace_id, seq).
type Ledgers interface {
	FindByOrgAndProductAndSiteAndWorkspaceAndSeq(ctx context.Context, orgID, productID, siteID uuid.UUID, seq int64) (functional.Option[model.Ledger], error)
	FindByOrgAndProductAndSiteAndSeq(ctx context.Context, orgID, productID, siteID uuid.UUID, seq int64) ([]model.Ledger, error)
	DeleteByOrgAndProductAndSiteAndSeq(ctx context.Context, orgID, productID, siteID uuid.UUID, seq int64) error
	FindByWorkspaceIDAndSeq(ctx context.Context, workspaceID string, seq int64) ([]model.Ledger, error)
	DeleteByWorkspaceIDAndSeq(ctx context.Context, workspaceID string, seq int64) (int64, error)
	DeleteByOrgAndMemoInternal(ctx context.Context, orgID uuid.UUID, memo string) (int64, error)
}

// Notes has the NOT NULL context scope workspace_id as its only scope.
type Notes interface {
	FindByID(ctx context.Context, id string) (functional.Option[model.Note], error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByIDInternal(ctx context.Context, id string) error
	FindByWorkspaceAndTitle(ctx context.Context, title string) (functional.Option[model.Note], error)
	FindAll(ctx context.Context) ([]model.Note, error)
}

// Drafts has the nullable context scope workspace_id as its only scope.
type Drafts interface {
	FindByID(ctx context.Context, id string) (functional.Option[model.Draft], error)
	DeleteByID(ctx context.Context, id string) error
	FindByTitle(ctx context.Context, title string) ([]model.Draft, error)
}

// SharedDocs has a nullable hard scope, org_id: a name gives it.
type SharedDocs interface {
	FindByID(ctx context.Context, id string) (functional.Option[model.SharedDoc], error)
	DeleteByID(ctx context.Context, id string) (bool, error)
	DeleteByTitleInternal(ctx context.Context, title string) (int64, error)
	FindAll(ctx context.Context) ([]model.SharedDoc, error)
}

// Zones has the hard scope org_id and the context scopes workspace_id and region_id.
type Zones interface {
	FindByOrgAndWorkspaceAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.Zone, error)
	DeleteByOrgAndWorkspaceAndName(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	FindByOrgAndRegionAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.Zone, error)
}

// Mixeds has org_id NOT NULL and product_id nullable, both hard scopes.
type Mixeds interface {
	FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id string) (functional.Option[model.Mixed], error)
	DeleteByOrgAndName(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	FindByProductAndName(ctx context.Context, productID uuid.UUID, name string) ([]model.Mixed, error)
}

// Bins has three hard scopes that imply each other from the narrowest.
type Bins interface {
	FindByProductAndID(ctx context.Context, productID uuid.UUID, id string) (functional.Option[model.Bin], error)
	FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id string) (functional.Option[model.Bin], error)
	DeleteByProductAndName(ctx context.Context, productID uuid.UUID, name string) (int64, error)
}

// Shelves has two hard scopes and no foreign key between them: Postgres.ScopeImplies
// says product_id determines org_id, and this table does not prove it.
type Shelves interface {
	FindByProductAndID(ctx context.Context, productID uuid.UUID, id string) (functional.Option[model.Shelf], error)
	FindByProductAndName(ctx context.Context, productID uuid.UUID, name string) ([]model.Shelf, error)
	DeleteByProductAndName(ctx context.Context, productID uuid.UUID, name string) (int64, error)
}

// BinNulls has the foreign key that proves product_id -> org_id, but org_id is
// nullable: with MATCH SIMPLE a row (product, NULL) is legal, so the key proves
// nothing. BinNvs has the key NOT VALID: rows that predate it may break it.
type (
	BinNulls interface {
		FindByProductAndName(ctx context.Context, productID uuid.UUID, name string) ([]model.BinNull, error)
		DeleteByProductAndName(ctx context.Context, productID uuid.UUID, name string) (int64, error)
	}
	BinNvs interface {
		FindByProductAndName(ctx context.Context, productID uuid.UUID, name string) ([]model.BinNv, error)
		DeleteByProductAndName(ctx context.Context, productID uuid.UUID, name string) (int64, error)
	}
)

// InternalWrites end in Internal: a derived write never skips a scope.
type InternalWrites interface {
	CreateInOrgAndProductAndWorkspaceInternal(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
	UpdateByOrgAndProductAndWorkspaceAndIDInternal(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
	UpsertByOrgAndProductAndWorkspaceAndSlugInternal(
		ctx context.Context, orgID, productID uuid.UUID, wide model.Wide,
	) (model.Wide, error)
}

// WorkspaceForms name a context scope by its Go field: allowed on a read, refused
// on a delete and on every write.
type (
	LedgerForms interface {
		DeleteByOrgAndProductAndSiteAndWorkspaceIDAndSeq(
			ctx context.Context, orgID, productID, siteID uuid.UUID, workspaceID string, seq int64,
		) (bool, error)
	}
	NoteForms interface {
		DeleteByWorkspaceIDAndID(ctx context.Context, workspaceID, id string) error
	}
	OrderForms interface {
		DeleteByOrgAndProductAndWorkspaceIDAndName(
			ctx context.Context, orgID, productID uuid.UUID, workspaceID, name string,
		) (int64, error)
	}
	WideForms interface {
		CreateInOrgAndProductAndWorkspaceID(
			ctx context.Context, orgID, productID uuid.UUID, workspaceID string, wide model.Wide,
		) (model.Wide, error)
		UpdateByOrgAndProductAndWorkspaceIDAndID(
			ctx context.Context, orgID, productID uuid.UUID, workspaceID string, wide model.Wide,
		) (model.Wide, error)
		UpsertByOrgAndProductAndWorkspaceIDAndSlug(
			ctx context.Context, orgID, productID uuid.UUID, workspaceID string, wide model.Wide,
		) (model.Wide, error)
	}
)

// BinWrites write a table whose site implies its product and org (Postgres.ScopeImplies,
// proved by foreign keys) for reads: a write never relaxes the rule, so the name
// gives every scope column, or product_id and org_id would come from the value.
type BinWrites interface {
	CreateInSite(ctx context.Context, siteID uuid.UUID, bin model.Bin) (model.Bin, error)
	CreateInProductAndSite(ctx context.Context, productID, siteID uuid.UUID, bin model.Bin) (model.Bin, error)
	UpdateBySiteAndID(ctx context.Context, siteID uuid.UUID, bin model.Bin) (model.Bin, error)
	UpdateByProductAndSiteAndID(ctx context.Context, productID, siteID uuid.UUID, bin model.Bin) (model.Bin, error)
}

// GlobalWrites write a table whose only unique keys, (id) and (slug), hold no scope column:
// an upsert would conflict across tenants.
type GlobalWrites interface {
	UpsertByOrgAndProductAndSlug(ctx context.Context, orgID, productID uuid.UUID, g model.Wglobal) error
	UpsertByOrgAndProductAndID(ctx context.Context, orgID, productID uuid.UUID, g model.Wglobal) error
}

// ViewWrites write a view: refused, like a derived read of a view.
type ViewWrites interface {
	CreateInOrgAndProduct(ctx context.Context, orgID, productID uuid.UUID, v model.OrderLive) (model.OrderLive, error)
	UpdateByOrgAndProductAndID(ctx context.Context, orgID, productID uuid.UUID, v model.OrderLive) (model.OrderLive, error)
	UpsertByOrgAndProductAndID(ctx context.Context, orgID, productID uuid.UUID, v model.OrderLive) (model.OrderLive, error)
}

// OrderLives is a view of orders: every derived name is refused, the scope
// columns of a view being nullable to the generator.
type OrderLives interface {
	FindByOrgAndProductAndID(ctx context.Context, orgID, productID uuid.UUID, id string) ([]model.OrderLive, error)
	FindByID(ctx context.Context, id string) ([]model.OrderLive, error)
	DeleteByID(ctx context.Context, id string) error
	FindAll(ctx context.Context) ([]model.OrderLive, error)
}

// LogLines has a scope and no key.
type LogLines interface {
	FindByOrgAndMsg(ctx context.Context, orgID uuid.UUID, msg string) (functional.Option[model.LogLine], error)
	GetByOrgAndMsg(ctx context.Context, orgID uuid.UUID, msg string) (model.LogLine, error)
}

// Kitchens has columns with no equality predicate, and parameters of the wrong type.
type Kitchens interface {
	FindByOrgAndCInet(ctx context.Context, orgID uuid.UUID, v string) ([]model.Kitchen, error)
	FindByOrgAndCJSON(ctx context.Context, orgID uuid.UUID, v string) ([]model.Kitchen, error)
	FindByOrgAndCBytes(ctx context.Context, orgID uuid.UUID, v []byte) ([]model.Kitchen, error)
	FindByOrgAndCTexts(ctx context.Context, orgID uuid.UUID, v pq.StringArray) ([]model.Kitchen, error)
	FindByOrgAndCInts(ctx context.Context, orgID uuid.UUID, v pq.Int32Array) ([]model.Kitchen, error)
	ExistsByOrgAndNInet(ctx context.Context, orgID uuid.UUID, v string) (bool, error)
	DeleteByOrgAndNJSON(ctx context.Context, orgID uuid.UUID, v string) (int64, error)
	DeleteByOrgAndNBytes(ctx context.Context, orgID uuid.UUID, v []byte) (int64, error)
	FindByOrgAndNTexts(ctx context.Context, orgID uuid.UUID, v pq.StringArray) ([]model.Kitchen, error)
	FindByOrgAndNInts(ctx context.Context, orgID uuid.UUID, v pq.Int32Array) ([]model.Kitchen, error)
	FindByOrgAndCUUID(ctx context.Context, orgID uuid.UUID, v string) ([]model.Kitchen, error)
	FindByOrgAndCInt(ctx context.Context, orgID uuid.UUID, v int) ([]model.Kitchen, error)
	FindByOrgAndCSmall(ctx context.Context, orgID uuid.UUID, v int32) ([]model.Kitchen, error)
	FindByOrgAndCText(ctx context.Context, orgID uuid.UUID, v *string) ([]model.Kitchen, error)
	FindByOrgAndCTstz(ctx context.Context, orgID uuid.UUID, v string) ([]model.Kitchen, error)
	FindByOrgAndCMood(ctx context.Context, orgID uuid.UUID, v string) ([]model.Kitchen, error)
	FindByOrgAndCReal(ctx context.Context, orgID uuid.UUID, v float64) ([]model.Kitchen, error)
}

// Collides, CollideWorkspaces and CollideProducts have a column whose Go
// field is a short scope name: Org, Workspace, Product. The name would be
// ambiguous, so the table is refused where it is bound.
type (
	Collides interface {
		FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id string) (functional.Option[model.Collide], error)
	}
	CollideWorkspaces interface {
		FindByWorkspaceAndID(ctx context.Context, id string) (functional.Option[model.CollideWs], error)
	}
	CollideProducts interface {
		FindByOrgAndProductAndID(ctx context.Context, orgID, productID uuid.UUID, id string) (functional.Option[model.CollideProduct], error)
	}
)

func _() {
	nanorepo.Implement[Users, usersImpl](table.User, nanorepo.NoMapper[model.User])
	nanorepo.Implement[Orders, ordersImpl](table.Order, nanorepo.NoMapper[model.Order])
	nanorepo.Implement[Ledgers, ledgersImpl](table.Ledger, nanorepo.NoMapper[model.Ledger])
	nanorepo.Implement[Notes, notesImpl](table.Note, nanorepo.NoMapper[model.Note])
	nanorepo.Implement[Drafts, draftsImpl](table.Draft, nanorepo.NoMapper[model.Draft])
	nanorepo.Implement[SharedDocs, sharedDocsImpl](table.SharedDoc, nanorepo.NoMapper[model.SharedDoc])
	nanorepo.Implement[Zones, zonesImpl](table.Zone, nanorepo.NoMapper[model.Zone])
	nanorepo.Implement[Mixeds, mixedsImpl](table.Mixed, nanorepo.NoMapper[model.Mixed])
	nanorepo.Implement[Bins, binsImpl](table.Bin, nanorepo.NoMapper[model.Bin])
	nanorepo.Implement[OrderLives, orderLivesImpl](view.OrderLive, nanorepo.NoMapper[model.OrderLive])
	nanorepo.Implement[LogLines, logLinesImpl](table.LogLine, nanorepo.NoMapper[model.LogLine])
	nanorepo.Implement[Kitchens, kitchensImpl](table.Kitchen, nanorepo.NoMapper[model.Kitchen])
	nanorepo.Implement[Shelves, shelvesImpl](table.Shelf, nanorepo.NoMapper[model.Shelf])
	nanorepo.Implement[LedgerForms, ledgerFormsImpl](table.Ledger, nanorepo.NoMapper[model.Ledger])
	nanorepo.Implement[NoteForms, noteFormsImpl](table.Note, nanorepo.NoMapper[model.Note])
	nanorepo.Implement[OrderForms, orderFormsImpl](table.Order, nanorepo.NoMapper[model.Order])
	nanorepo.Implement[WideForms, wideFormsImpl](table.Wide, nanorepo.NoMapper[model.Wide])
	nanorepo.Implement[BinWrites, binWritesImpl](table.Bin, nanorepo.NoMapper[model.Bin])
	nanorepo.Implement[GlobalWrites, globalWritesImpl](table.Wglobal, nanorepo.NoMapper[model.Wglobal])
	nanorepo.Implement[ViewWrites, viewWritesImpl](view.OrderLive, nanorepo.NoMapper[model.OrderLive])
	nanorepo.Implement[BinNulls, binNullsImpl](table.BinNull, nanorepo.NoMapper[model.BinNull])
	nanorepo.Implement[BinNvs, binNvsImpl](table.BinNv, nanorepo.NoMapper[model.BinNv])
	nanorepo.Implement[InternalWrites, internalWritesImpl](table.Wide, nanorepo.NoMapper[model.Wide])
	nanorepo.Implement[Collides, collidesImpl](table.Collide, nanorepo.NoMapper[model.Collide])
	nanorepo.Implement[CollideWorkspaces, collideWorkspacesImpl](table.CollideWs, nanorepo.NoMapper[model.CollideWs])
	nanorepo.Implement[CollideProducts, collideProductsImpl](table.CollideProduct, nanorepo.NoMapper[model.CollideProduct])
}

type (
	usersImpl             struct{ userBase }
	ordersImpl            struct{ orderBase }
	ledgersImpl           struct{ ledgerBase }
	notesImpl             struct{ noteBase }
	draftsImpl            struct{ draftBase }
	logLinesImpl          struct{ logLineBase }
	sharedDocsImpl        struct{ sharedDocBase }
	zonesImpl             struct{ zoneBase }
	mixedsImpl            struct{ mixedBase }
	binsImpl              struct{ binBase }
	orderLivesImpl        struct{ orderLiveBase }
	kitchensImpl          struct{ kitchenBase }
	shelvesImpl           struct{ shelfBase }
	ledgerFormsImpl       struct{ ledgerBase }
	noteFormsImpl         struct{ noteBase }
	orderFormsImpl        struct{ orderBase }
	wideFormsImpl         struct{ wideBase }
	binWritesImpl         struct{ binBase }
	globalWritesImpl      struct{ wglobalBase }
	viewWritesImpl        struct{ orderLiveBase }
	binNullsImpl          struct{ binNullBase }
	binNvsImpl            struct{ binNvBase }
	internalWritesImpl    struct{ wideBase }
	collidesImpl          struct{ collideBase }
	collideWorkspacesImpl struct{ collideWsBase }
	collideProductsImpl   struct{ collideProductBase }
)

// New is here so that the package compiles.
func New(db qrm.DB) {}
