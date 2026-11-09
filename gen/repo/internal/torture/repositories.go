package torture

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// OrgID is a named type over the model type of org_id: a derived method
// converts it.
type OrgID uuid.UUID

// Handle is a named type over string.
type Handle string

// Groups has no scope column: nothing filters a tenant, whatever the name.
type Groups interface {
	FindByID(ctx context.Context, id int64) (functional.Option[model.Group], error)
	GetByID(ctx context.Context, id int64) (model.Group, error)
	FindBySlug(ctx context.Context, slug string) (functional.Option[model.Group], error)
	FindByName(ctx context.Context, name string) ([]model.Group, error)
	FindByNameAndNameKey(ctx context.Context, name, nameKey string) ([]model.Group, error)
	FindByNameKey(ctx context.Context, nameKey string) ([]model.Group, error)
	FindByCount(ctx context.Context, count int32) ([]model.Group, error)
	FindByBy(ctx context.Context, by string) ([]model.Group, error)
	FindByAnd(ctx context.Context, and string) ([]model.Group, error)
	FindByByAndAnd(ctx context.Context, by, and string) ([]model.Group, error)
	FindByIsInternal(ctx context.Context, isInternal bool) ([]model.Group, error)
	FindByDeletedAt(ctx context.Context, deletedAt time.Time) ([]model.Group, error)
	FindByNameInternal(ctx context.Context, name string) ([]model.Group, error)
	FindAll(ctx context.Context) ([]model.Group, error)
	FindAllInternal(ctx context.Context) ([]model.Group, error)
	ExistsByName(ctx context.Context, name string) (bool, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	CountByName(ctx context.Context, name string) (int64, error)
	CountByIsInternal(ctx context.Context, isInternal bool) (int64, error)
	Count(ctx context.Context) (int64, error)
	CountInternal(ctx context.Context) (int64, error)
	DeleteByID(ctx context.Context, id int64) error
	DeleteBySlug(ctx context.Context, slug string) (bool, error)
	DeleteByName(ctx context.Context, name string) (int64, error)
	DeleteByNameAndNameKey(ctx context.Context, name, nameKey string) (int64, error)
}

// Users has one hard scope, org_id. Its partial and expression unique
// indexes are not keys; its unique key on external_id is on a nullable
// column.
type Users interface {
	FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (functional.Option[model.User], error)
	GetByOrgAndID(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (model.User, error)
	GetByOrgIDAndID(ctx context.Context, orgID OrgID, id uuid.UUID) (model.User, error)
	FindByOrgAndExternalID(
		ctx context.Context,
		orgID uuid.UUID,
		externalID string,
	) (functional.Option[model.User], error)
	FindByOrgAndNameAndNameKey(
		ctx context.Context,
		orgID uuid.UUID,
		name, nameKey string,
	) (functional.Option[model.User], error)
	FindByOrgAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgAndHandle(ctx context.Context, orgID uuid.UUID, handle Handle) ([]model.User, error)
	FindByOrgAndEmail(ctx context.Context, orgID uuid.UUID, email string) ([]model.User, error)
	FindByOrgAndMixedCase(ctx context.Context, orgID uuid.UUID, mixedCase string) ([]model.User, error)
	FindByOrgAndInternal(ctx context.Context, orgID uuid.UUID, internal bool) ([]model.User, error)
	FindByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) ([]model.User, error)
	FindByOrgInternal(ctx context.Context, orgID uuid.UUID) ([]model.User, error)
	FindByInternalInternal(ctx context.Context, internal bool) ([]model.User, error)
	FindByNameInternal(ctx context.Context, name string) ([]model.User, error)
	FindByIDInternal(ctx context.Context, id uuid.UUID) (functional.Option[model.User], error)
	FindAllInternal(ctx context.Context) ([]model.User, error)
	CountInternal(ctx context.Context) (int64, error)
	CountByNameInternal(ctx context.Context, name string) (int64, error)
	ExistsByNameInternal(ctx context.Context, name string) (bool, error)
	CountByOrgIDAndHandle(ctx context.Context, orgID OrgID, handle Handle) (int64, error)
	DeleteByOrgIDAndHandle(ctx context.Context, orgID OrgID, handle Handle) (int64, error)
	CountByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	CountByOrgAndInternal(ctx context.Context, orgID uuid.UUID, internal bool) (int64, error)
	ExistsByOrgAndEmail(ctx context.Context, orgID uuid.UUID, email string) (bool, error)
	ExistsByOrgAndID(ctx context.Context, orgID uuid.UUID, id uuid.UUID) (bool, error)
	FindByOrg(ctx context.Context, orgID uuid.UUID) ([]model.User, error)
	DeleteByOrgAndID(ctx context.Context, orgID uuid.UUID, id uuid.UUID) error
	DeleteByOrgAndExternalID(ctx context.Context, orgID uuid.UUID, externalID string) (bool, error)
	DeleteByOrgAndName(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
}

// Orders has two hard scopes (org_id, product_id) and a nullable context
// scope (workspace_id). Workspace binds no parameter; WorkspaceID does.
type Orders interface {
	FindByOrgAndProductAndWorkspaceAndID(
		ctx context.Context,
		orgID, productID uuid.UUID,
		id uuid.UUID,
	) (functional.Option[model.Order], error)
	GetByOrgAndProductAndWorkspaceAndID(
		ctx context.Context,
		orgID, productID uuid.UUID,
		id uuid.UUID,
	) (model.Order, error)
	FindByOrgAndProductAndWorkspaceAndBy(
		ctx context.Context,
		orgID, productID uuid.UUID,
		by string,
	) (functional.Option[model.Order], error)
	FindByOrgAndProductAndWorkspaceIDAndName(
		ctx context.Context,
		orgID, productID uuid.UUID,
		workspaceID, name string,
	) (functional.Option[model.Order], error)
	FindByOrgIDAndProductIDAndWorkspaceAndBy(
		ctx context.Context,
		orgID, productID uuid.UUID,
		by string,
	) (functional.Option[model.Order], error)
	FindByOrgAndProductAndWorkspaceAndName(
		ctx context.Context,
		orgID, productID uuid.UUID,
		name string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndNameAndNameKey(
		ctx context.Context,
		orgID, productID uuid.UUID,
		name, nameKey string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndBrandAndModel(
		ctx context.Context,
		orgID, productID uuid.UUID,
		brandAndModel string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndIDAndName(
		ctx context.Context,
		orgID, productID uuid.UUID,
		idAndName string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndCount(
		ctx context.Context,
		orgID, productID uuid.UUID,
		count int32,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndAnd(
		ctx context.Context,
		orgID, productID uuid.UUID,
		and string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndInternal(
		ctx context.Context,
		orgID, productID uuid.UUID,
		internal bool,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndIsInternal(
		ctx context.Context,
		orgID, productID uuid.UUID,
		isInternal bool,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspaceAndOrderBy(
		ctx context.Context,
		orgID, productID uuid.UUID,
		orderBy string,
	) ([]model.Order, error)
	FindByOrgAndProductAndWorkspace(ctx context.Context, orgID, productID uuid.UUID) ([]model.Order, error)
	FindByOrgAndWorkspaceAndProduct(ctx context.Context, orgID, productID uuid.UUID) ([]model.Order, error)
	FindByOrgAndProductIDAndWorkspaceIDAndName(
		ctx context.Context,
		orgID, productID uuid.UUID,
		workspaceID, name string,
	) ([]model.Order, error)
	FindByOrgAndProductAndNameInternal(
		ctx context.Context,
		orgID, productID uuid.UUID,
		name string,
	) ([]model.Order, error)
	FindByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) ([]model.Order, error)
	CountByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	ExistsByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) (bool, error)
	FindByNameInternal(ctx context.Context, name string) ([]model.Order, error)
	FindByIDInternal(ctx context.Context, id uuid.UUID) (functional.Option[model.Order], error)
	FindAllInternal(ctx context.Context) ([]model.Order, error)
	CountInternal(ctx context.Context) (int64, error)
	CountByOrgAndProductAndWorkspace(ctx context.Context, orgID, productID uuid.UUID) (int64, error)
	CountByOrgAndProductAndWorkspaceAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (int64, error)
	ExistsByOrgAndProductAndWorkspaceAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (bool, error)
	ExistsByOrgAndProductAndWorkspaceAndID(ctx context.Context, orgID, productID uuid.UUID, id uuid.UUID) (bool, error)
	DeleteByOrgAndProductAndWorkspaceAndID(ctx context.Context, orgID, productID uuid.UUID, id uuid.UUID) (bool, error)
	DeleteByOrgAndProductAndWorkspaceAndBy(ctx context.Context, orgID, productID uuid.UUID, by string) error
	DeleteByOrgAndProductAndWorkspaceAndName(
		ctx context.Context,
		orgID, productID uuid.UUID,
		name string,
	) (int64, error)
	DeleteByOrgAndProductAndWorkspace(ctx context.Context, orgID, productID uuid.UUID) (int64, error)
}

// Ledgers has three hard scopes and a NOT NULL context scope, all in the
// primary key with seq.
type Ledgers interface {
	FindByOrgAndProductAndSiteAndWorkspaceIDAndSeq(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		workspaceID string,
		seq int64,
	) (functional.Option[model.Ledger], error)
	GetByOrgAndProductAndSiteAndWorkspaceIDAndSeq(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		workspaceID string,
		seq int64,
	) (model.Ledger, error)
	FindByOrgAndProductAndSiteAndWorkspace(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
	) ([]model.Ledger, error)
	FindByOrgAndProductAndSiteAndWorkspaceAndMemo(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		memo string,
	) ([]model.Ledger, error)
	FindByOrgAndProductAndSiteAndWorkspaceAndSeq(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		seq int64,
	) ([]model.Ledger, error)
	FindByOrgAndMemoInternal(ctx context.Context, orgID uuid.UUID, memo string) ([]model.Ledger, error)
	FindAllInternal(ctx context.Context) ([]model.Ledger, error)
	CountInternal(ctx context.Context) (int64, error)
	CountByOrgAndProductAndSiteAndWorkspace(ctx context.Context, orgID, productID, siteID uuid.UUID) (int64, error)
	CountByOrgAndProductAndSiteAndWorkspaceIDAndSeq(
		ctx context.Context, orgID, productID, siteID uuid.UUID, workspaceID string, seq int64,
	) (int64, error)
	ExistsByOrgAndProductAndSiteAndWorkspaceIDAndSeq(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		workspaceID string,
		seq int64,
	) (bool, error)
	DeleteByOrgAndProductAndSiteAndWorkspaceAndSeq(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		seq int64,
	) (int64, error)
	DeleteByOrgAndProductAndSiteAndWorkspaceAndMemo(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		memo string,
	) error
}

// Notes has one scope, the NOT NULL context column workspace_id: with no
// workspace in ctx, the registered function keeps every row.
type Notes interface {
	FindByWorkspaceAndID(ctx context.Context, id string) (functional.Option[model.Note], error)
	GetByWorkspaceAndID(ctx context.Context, id string) (model.Note, error)
	FindByWorkspaceIDAndID(ctx context.Context, workspaceID, id string) (functional.Option[model.Note], error)
	FindByWorkspaceAndTitle(ctx context.Context, title string) ([]model.Note, error)
	FindByWorkspace(ctx context.Context) ([]model.Note, error)
	CountByWorkspace(ctx context.Context) (int64, error)
	ExistsByWorkspaceAndTitle(ctx context.Context, title string) (bool, error)
	DeleteByWorkspaceAndID(ctx context.Context, id string) (bool, error)
	DeleteByWorkspaceAndTitle(ctx context.Context, title string) (int64, error)
	FindByIDInternal(ctx context.Context, id string) (functional.Option[model.Note], error)
	FindAllInternal(ctx context.Context) ([]model.Note, error)
	CountInternal(ctx context.Context) (int64, error)
}

// Drafts has one scope, the nullable context column workspace_id.
type Drafts interface {
	FindByWorkspaceAndID(ctx context.Context, id string) (functional.Option[model.Draft], error)
	FindByWorkspaceAndTitle(ctx context.Context, title string) ([]model.Draft, error)
	CountByWorkspace(ctx context.Context) (int64, error)
	DeleteByWorkspaceAndTitle(ctx context.Context, title string) (int64, error)
	FindByIDInternal(ctx context.Context, id string) (functional.Option[model.Draft], error)
}

// SharedDocs has a nullable org_id that is a hard scope: a derived name must
// name it, with equality, so a row with no org matches no name; a read may end
// in Internal.
type SharedDocs interface {
	FindByOrgAndID(ctx context.Context, orgID uuid.UUID, id string) (functional.Option[model.SharedDoc], error)
	FindByOrgAndTitle(ctx context.Context, orgID uuid.UUID, title string) ([]model.SharedDoc, error)
	FindByOrg(ctx context.Context, orgID uuid.UUID) ([]model.SharedDoc, error)
	CountByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	DeleteByOrgAndID(ctx context.Context, orgID uuid.UUID, id string) (bool, error)
	DeleteByOrgIDAndTitle(ctx context.Context, orgID uuid.UUID, title string) (int64, error)
	FindByIDInternal(ctx context.Context, id string) (functional.Option[model.SharedDoc], error)
	FindAllInternal(ctx context.Context) ([]model.SharedDoc, error)
}

// LogLines has a scope and no key.
type LogLines interface {
	FindByOrg(ctx context.Context, orgID uuid.UUID) ([]model.LogLine, error)
	FindByOrgAndMsg(ctx context.Context, orgID uuid.UUID, msg string) ([]model.LogLine, error)
	CountByOrg(ctx context.Context, orgID uuid.UUID) (int64, error)
	ExistsByOrgAndMsg(ctx context.Context, orgID uuid.UUID, msg string) (bool, error)
	DeleteByOrgAndMsg(ctx context.Context, orgID uuid.UUID, msg string) (int64, error)
}

// Zones has two context scopes and a hard one: a name gives all three.
type Zones interface {
	FindByOrgAndWorkspaceAndRegionAndID(
		ctx context.Context,
		orgID uuid.UUID,
		id string,
	) (functional.Option[model.Zone], error)
	FindByOrgAndRegionAndWorkspaceAndName(ctx context.Context, orgID uuid.UUID, name string) ([]model.Zone, error)
	CountByOrgAndWorkspaceAndRegion(ctx context.Context, orgID uuid.UUID) (int64, error)
	ExistsByOrgAndWorkspaceAndRegionAndName(ctx context.Context, orgID uuid.UUID, name string) (bool, error)
	DeleteByOrgAndWorkspaceAndRegionAndName(ctx context.Context, orgID uuid.UUID, name string) (int64, error)
	DeleteByOrgAndWorkspaceAndRegionAndID(ctx context.Context, orgID uuid.UUID, id string) (bool, error)
	FindByOrgAndWorkspaceIDAndRegionAndName(
		ctx context.Context,
		orgID uuid.UUID,
		workspaceID, name string,
	) ([]model.Zone, error)
	FindByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) ([]model.Zone, error)
}

// Mixeds has a NOT NULL hard scope (org_id) and a nullable one (product_id).
type Mixeds interface {
	FindByOrgAndProductAndID(
		ctx context.Context,
		orgID, productID uuid.UUID,
		id string,
	) (functional.Option[model.Mixed], error)
	FindByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) ([]model.Mixed, error)
	CountByOrgAndProduct(ctx context.Context, orgID, productID uuid.UUID) (int64, error)
	DeleteByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (int64, error)
	FindByOrgAndNameInternal(ctx context.Context, orgID uuid.UUID, name string) ([]model.Mixed, error)
}

// Bins has three hard scopes that determine each other (ScopeImplies): the
// narrowest one named stands for the others.
type Bins interface {
	FindBySiteAndID(ctx context.Context, siteID uuid.UUID, id string) (functional.Option[model.Bin], error)
	FindByOrgAndProductAndSiteAndName(
		ctx context.Context,
		orgID, productID, siteID uuid.UUID,
		name string,
	) ([]model.Bin, error)
	CountBySite(ctx context.Context, siteID uuid.UUID) (int64, error)
	DeleteBySiteAndName(ctx context.Context, siteID uuid.UUID, name string) (int64, error)
}

// Shelves has two hard scopes, and no foreign key proves that a product
// belongs to one org: a name gives both, whatever Postgres.ScopeImplies says.
type Shelves interface {
	FindByOrgAndProductAndID(
		ctx context.Context, orgID, productID uuid.UUID, id string,
	) (functional.Option[model.Shelf], error)
	FindByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) ([]model.Shelf, error)
	DeleteByOrgAndProductAndName(ctx context.Context, orgID, productID uuid.UUID, name string) (int64, error)
}

// BinFulls has a nullable org_id and the MATCH FULL key that proves
// product_id -> org_id for every row: Product alone names both.
type BinFulls interface {
	FindByProductAndName(ctx context.Context, productID uuid.UUID, name string) ([]model.BinFull, error)
	DeleteByProductAndName(ctx context.Context, productID uuid.UUID, name string) (int64, error)
}

// Members is a join table: the tenant column is part of its composite key.
type Members interface {
	FindByOrgAndGroupIDAndUserID(
		ctx context.Context,
		orgID uuid.UUID,
		groupID int64,
		userID uuid.UUID,
	) (functional.Option[model.Member], error)
	FindByOrgAndUserID(ctx context.Context, orgID uuid.UUID, userID uuid.UUID) ([]model.Member, error)
	CountByOrgAndGroupID(ctx context.Context, orgID uuid.UUID, groupID int64) (int64, error)
	DeleteByOrgAndGroupID(ctx context.Context, orgID uuid.UUID, groupID int64) (int64, error)
	DeleteByOrgAndGroupIDAndUserID(ctx context.Context, orgID uuid.UUID, groupID int64, userID uuid.UUID) (bool, error)
}

func _() {
	nanorepo.Implement[Groups, groupsImpl](table.Group, nanorepo.NoMapper[model.Group])
	nanorepo.Implement[Users, usersImpl](table.User, nanorepo.NoMapper[model.User])
	nanorepo.Implement[Orders, ordersImpl](table.Order, nanorepo.NoMapper[model.Order])
	nanorepo.Implement[Ledgers, ledgersImpl](table.Ledger, nanorepo.NoMapper[model.Ledger])
	nanorepo.Implement[Notes, notesImpl](table.Note, nanorepo.NoMapper[model.Note])
	nanorepo.Implement[Drafts, draftsImpl](table.Draft, nanorepo.NoMapper[model.Draft])
	nanorepo.Implement[SharedDocs, sharedDocsImpl](table.SharedDoc, nanorepo.NoMapper[model.SharedDoc])
	nanorepo.Implement[LogLines, logLinesImpl](table.LogLine, nanorepo.NoMapper[model.LogLine])
	nanorepo.Implement[Zones, zonesImpl](table.Zone, nanorepo.NoMapper[model.Zone])
	nanorepo.Implement[Mixeds, mixedsImpl](table.Mixed, nanorepo.NoMapper[model.Mixed])
	nanorepo.Implement[Bins, binsImpl](table.Bin, nanorepo.NoMapper[model.Bin])
	nanorepo.Implement[Shelves, shelvesImpl](table.Shelf, nanorepo.NoMapper[model.Shelf])
	nanorepo.Implement[BinFulls, binFullsImpl](table.BinFull, nanorepo.NoMapper[model.BinFull])
	nanorepo.Implement[Members, membersImpl](table.Member, nanorepo.NoMapper[model.Member])
	// Tables whose columns collide with a short scope name: bound here for
	// their base only, and refused where a repository binds them
	// (testdata/torture/refused).
	nanorepo.Base[model.Collide](table.Collide)
	nanorepo.Base[model.CollideWs](table.CollideWs)
	nanorepo.Base[model.CollideProduct](table.CollideProduct)
}

type (
	groupsImpl     struct{ groupBase }
	usersImpl      struct{ userBase }
	ordersImpl     struct{ orderBase }
	ledgersImpl    struct{ ledgerBase }
	notesImpl      struct{ noteBase }
	draftsImpl     struct{ draftBase }
	sharedDocsImpl struct{ sharedDocBase }
	logLinesImpl   struct{ logLineBase }
	zonesImpl      struct{ zoneBase }
	mixedsImpl     struct{ mixedBase }
	binsImpl       struct{ binBase }
	membersImpl    struct{ memberBase }
	shelvesImpl    struct{ shelfBase }
)

// NewGroups returns the groups on db.
func NewGroups(db qrm.DB) Groups { return &groupsImpl{groupBase: newGroupBase(db)} }

// NewUsers returns the users on db.
func NewUsers(db qrm.DB) Users { return &usersImpl{userBase: newUserBase(db)} }

// NewOrders returns the orders on db.
func NewOrders(db qrm.DB) Orders { return &ordersImpl{orderBase: newOrderBase(db)} }

// NewLedgers returns the ledgers on db.
func NewLedgers(db qrm.DB) Ledgers { return &ledgersImpl{ledgerBase: newLedgerBase(db)} }

// NewNotes returns the notes on db.
func NewNotes(db qrm.DB) Notes { return &notesImpl{noteBase: newNoteBase(db)} }

// NewDrafts returns the drafts on db.
func NewDrafts(db qrm.DB) Drafts { return &draftsImpl{draftBase: newDraftBase(db)} }

// NewSharedDocs returns the shared docs on db.
func NewSharedDocs(db qrm.DB) SharedDocs { return &sharedDocsImpl{sharedDocBase: newSharedDocBase(db)} }

// NewLogLines returns the log lines on db.
func NewLogLines(db qrm.DB) LogLines { return &logLinesImpl{logLineBase: newLogLineBase(db)} }

// NewZones returns the zones on db.
func NewZones(db qrm.DB) Zones { return &zonesImpl{zoneBase: newZoneBase(db)} }

// NewMixeds returns the mixeds on db.
func NewMixeds(db qrm.DB) Mixeds { return &mixedsImpl{mixedBase: newMixedBase(db)} }

// NewBins returns the bins on db.
func NewBins(db qrm.DB) Bins { return &binsImpl{binBase: newBinBase(db)} }

type binFullsImpl struct{ binFullBase }

// NewBinFulls returns the bin_full rows on db.
func NewBinFulls(db qrm.DB) BinFulls { return &binFullsImpl{binFullBase: newBinFullBase(db)} }

// NewMembers returns the members on db.
func NewMembers(db qrm.DB) Members { return &membersImpl{memberBase: newMemberBase(db)} }

// NewShelves returns the shelves on db.
func NewShelves(db qrm.DB) Shelves { return &shelvesImpl{shelfBase: newShelfBase(db)} }
