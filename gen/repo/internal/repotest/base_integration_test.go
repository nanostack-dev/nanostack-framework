//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	gendb "github.com/nanostack-dev/nanostack-framework/gen/db"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// These tests run every helper of the generated base on PostgreSQL: a
// container of the fixture's image, or the server NANO_GEN_DATABASE_URL
// names. Run them with: go test -tags integration ./repo/...

// shop is the declaration of the fixture, as a service would write it.
var shop = project.Postgres{Name: "shop", Out: "db"} //nolint:gochecknoglobals // fixture

var server = sync.OnceValue(func() *gendb.Server { //nolint:gochecknoglobals // one server per test binary
	return gendb.NewServer(os.Getenv(gendb.EnvDatabaseURL), gendb.WithDefaults(shop).Image)
})

func TestMain(m *testing.M) {
	code := m.Run()
	server().Close()
	os.Exit(code)
}

// The committed go-jet code of the fixture is what its migrations produce.
func TestSchemaIsCurrent(t *testing.T) {
	err := gendb.Run(gendb.Options{Dir: ".", Postgres: shop, Fresh: true, Check: true, Database: server().Open})
	if err != nil {
		t.Fatalf("%v\nregenerate with: go run ./cmd/nanostack gen db -dir repo/internal/repotest -name shop -out db "+
			"(from gen/), then go run ./cmd/nanostack gen repo -dir repo/internal/repotest -scopes tenant_id,workspace_id "+
			"-context-scopes workspace_id=github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Filter "+
			"-scope-names tenant_id=Tenant,workspace_id=Workspace "+
			"-context-stamps workspace_id=github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope.Stamp "+
			"-database-columns created_at,updated_at", err)
	}
}

// openShop returns a migrated scratch database.
func openShop(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, drop, err := server().Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(drop)
	cfg := nanomigrate.DefaultConfig()
	cfg.BasePath = "migrations"
	cfg.Validate = false
	if err = nanomigrate.NewMigrator(cfg, db, slog.New(slog.DiscardHandler)).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// fixture is a migrated database with two tenants and their items.
type fixture struct {
	db      *sql.DB
	repo    *itemRepositoryImpl
	tenant  uuid.UUID
	other   uuid.UUID
	created time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := openShop(t)
	f := &fixture{db: db, repo: NewItemRepository(db).(*itemRepositoryImpl), tenant: uuid.New(), other: uuid.New(),
		created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	ctx := context.Background()
	tenants := newTenantsBase(db)
	for i, id := range []uuid.UUID{f.tenant, f.other} {
		stmt := tenants.t.INSERT(tenants.t.AllColumns).
			MODEL(model.Tenants{ID: id, Name: "tenant " + string(rune('a'+i))}).
			RETURNING(tenants.t.AllColumns)
		if _, err := tenants.queryRow(ctx, stmt).Value(); err != nil {
			t.Fatalf("insert tenant: %v", err)
		}
	}
	weight := float32(1.5)
	released := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	local := time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC)
	opens := time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)
	external := "ext-1"
	for _, item := range []model.Items{
		{ID: "a1", TenantID: f.tenant, Sku: "A-1", Name: "Red 100% wool", Status: model.ItemStatus_Active, Stock: 3,
			Sold: 10, Rank: 1, Price: 9.5, Weight: &weight, Active: true, ReleasedOn: &released, LocalAt: &local,
			OpensAt: &opens, CreatedAt: f.created, ExternalID: &external},
		{ID: "a2", TenantID: f.tenant, Sku: "A-2", Name: "Red_cotton", Status: model.ItemStatus_Active, Stock: 0,
			Sold: 20, Rank: 2, Price: 19.5, Active: false, CreatedAt: f.created.Add(time.Hour)},
		{ID: "a3", TenantID: f.tenant, Sku: "A-3", Name: "Blue", Status: model.ItemStatus_Archived, Stock: 7,
			Sold: 30, Rank: 3, Price: 29.5, Active: true, CreatedAt: f.created.Add(2 * time.Hour)},
		{ID: "b1", TenantID: f.other, Sku: "A-1", Name: "Red", Status: model.ItemStatus_Active, Stock: 1, Active: true,
			CreatedAt: f.created},
	} {
		if _, err := f.repo.Create(ctx, item); err != nil {
			t.Fatalf("create %s: %v", item.ID, err)
		}
	}
	return f
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// Every predicate of the where type runs on PostgreSQL and selects the rows
// it names: each literal matches its column's type, the enum included.
func TestPredicatesSelectTheirRows(t *testing.T) {
	f := newFixture(t)
	w, ctx := f.repo.where, context.Background()
	released := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	for name, test := range map[string]struct {
		cond postgres.BoolExpression
		want []string
	}{
		"text":                  {w.Sku("A-2"), []string{"a2"}},
		"text in":               {w.IDIn("a1", "a3", "zz"), []string{"a1", "a3"}},
		"empty in":              {w.IDIn(), nil},
		"uuid":                  {w.TenantID(f.other), []string{"b1"}},
		"uuid in":               {w.TenantIDIn(f.other), []string{"b1"}},
		"enum":                  {w.Status(model.ItemStatus_Archived), []string{"a3"}},
		"enum in":               {w.StatusIn(model.ItemStatus_Archived, model.ItemStatus_Active), []string{"a1", "a2", "a3", "b1"}},
		"integer":               {w.Stock(7), []string{"a3"}},
		"bigint after":          {w.SoldAfter(15), []string{"a2", "a3"}},
		"smallint before":       {w.RankBefore(2), []string{"a1", "b1"}},
		"double":                {w.Price(19.5), []string{"a2"}},
		"real":                  {w.Weight(1.5), []string{"a1"}},
		"bool":                  {w.Active(false), []string{"a2"}},
		"date":                  {w.ReleasedOn(released), []string{"a1"}},
		"timestamp":             {w.LocalAtBefore(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)), []string{"a1"}},
		"time":                  {w.OpensAt(time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC)), []string{"a1"}},
		"timestamptz after":     {w.CreatedAtAfter(f.created), []string{"a2", "a3"}},
		"is null":               {w.ExternalIDIsNull().AND(w.TenantID(f.tenant)), []string{"a2", "a3"}},
		"array is null":         {w.TagsIsNull().AND(w.TenantID(f.other)), []string{"b1"}},
		"contains, any case":    {w.NameContains("RED"), []string{"a1", "a2", "b1"}},
		"contains % literally":  {w.NameContains("100%"), []string{"a1"}},
		"contains _ literally":  {w.NameContains("d_c"), []string{"a2"}},
		"equals, ignoring case": {w.NameEqualsIgnoreCase("BLUE"), []string{"a3"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.repo.findAll(ctx, test.cond, f.repo.t.ID.ASC())
			if err != nil {
				t.Fatalf("findAll: %v", err)
			}
			if !slices.Equal(ids(got), test.want) && !(len(got) == 0 && len(test.want) == 0) {
				t.Fatalf("got %v, want %v", ids(got), test.want)
			}
		})
	}
}

// Contains is searchexec.Contains on PostgreSQL: % _ ! and \ in the term
// match themselves, case does not count, the term is trimmed, and a blank
// term matches every row, also a row where the column is NULL. A blank
// Contains composes with AND and keeps the other condition.
func TestContainsMatchesLiterally(t *testing.T) {
	ctx := context.Background()
	b := newAuditEventsBase(newFixture(t).db) // events name existing items (audit_events_item_id_fkey)
	note := func(s string) *string { return &s }
	item := func(s string) *string { return &s }
	var all []int64
	for _, ev := range []model.AuditEvents{
		{ItemID: item("a1"), Action: "100% off"},
		{ItemID: item("a1"), Action: "a_b", Note: note("Late NOTE")},
		{ItemID: item("a2"), Action: "x!y", Note: note("on time")},
		{ItemID: item("a2"), Action: `c:\tmp`},
		{ItemID: item("a3"), Action: "AbC"},
	} {
		stmt := b.t.INSERT(b.t.ItemID, b.t.Action, b.t.Note).MODEL(ev).RETURNING(b.t.AllColumns)
		row, err := b.queryRow(ctx, stmt).Value()
		if err != nil {
			t.Fatalf("insert %q: %v", ev.Action, err)
		}
		all = append(all, row.ID)
	}
	w := b.where
	for name, test := range map[string]struct {
		cond postgres.BoolExpression
		want []int64
	}{
		"percent":             {w.ActionContains("%"), all[0:1]},
		"underscore":          {w.ActionContains("_"), all[1:2]},
		"escape character":    {w.ActionContains("!"), all[2:3]},
		"escape and a letter": {w.ActionContains("!y"), all[2:3]},
		"doubled escape":      {w.ActionContains("!!"), nil},
		"backslash":           {w.ActionContains(`\`), all[3:4]},
		"backslash and a t":   {w.ActionContains(`\t`), all[3:4]},
		"any case":            {w.ActionContains("aBc"), all[4:5]},
		"trimmed":             {w.ActionContains("  abc\t"), all[4:5]},
		"blank":               {w.ActionContains(""), all},
		"white space":         {w.ActionContains(" \t "), all},
		"blank on NULLs":      {w.NoteContains(""), all},
		"varchar, any case":   {w.NoteContains("note"), all[1:2]},
		"blank after AND":     {w.ItemID("a1").AND(w.ActionContains("")), all[0:2]},
		"blank before AND":    {w.ActionContains("").AND(w.ItemID("a2")), all[2:4]},
		"blank in OR":         {w.ItemID("a3").OR(w.NoteContains("")), all},
		"not valid UTF-8":     {w.ActionContains("\xff"), nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := b.findAll(ctx, test.cond, b.t.ID.ASC())
			if err != nil {
				t.Fatalf("findAll: %v", err)
			}
			gotIDs := make([]int64, len(got))
			for i, ev := range got {
				gotIDs[i] = ev.ID
			}
			if !slices.Equal(gotIDs, test.want) && !(len(gotIDs) == 0 && len(test.want) == 0) {
				t.Fatalf("got %v, want %v", gotIDs, test.want)
			}
		})
	}
}

// findOne reads LIMIT 2: no row is absent, one row is mapped, and two rows
// are ErrMoreThanOneRow, never a silent first row.
func TestFindOne(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	none, err := f.repo.findOne(ctx, w.ID("nope"))
	if err != nil || none.IsPresent() {
		t.Fatalf("no row: got %v, %v", none, err)
	}
	one, err := f.repo.findOne(ctx, w.TenantID(f.tenant).AND(w.Sku("A-1")))
	if err != nil || !one.IsPresent() || one.Value().ID != "a1" || one.Value().SKU != "A-1" {
		t.Fatalf("one row: got %v, %v", one, err)
	}
	_, err = f.repo.findOne(ctx, w.Sku("A-1"))
	if !errors.Is(err, transactor.ErrMoreThanOneRow) || err.Error() != transactor.ErrMoreThanOneRow.Error()+": items" {
		t.Fatalf("two rows: err = %v, want ErrMoreThanOneRow naming items", err)
	}
}

// first picks by the order; findAll returns every row in order, and no row
// is an empty slice.
func TestFirstAndFindAll(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	latest, err := f.repo.first(ctx, w.TenantID(f.tenant), f.repo.t.CreatedAt.DESC(), f.repo.t.ID.ASC())
	if err != nil || !latest.IsPresent() || latest.Value().ID != "a3" {
		t.Fatalf("first: got %v, %v", latest, err)
	}
	absent, err := f.repo.first(ctx, w.ID("nope"), f.repo.t.ID.ASC())
	if err != nil || absent.IsPresent() {
		t.Fatalf("first of nothing: got %v, %v", absent, err)
	}
	all, err := f.repo.findAll(ctx, nil, f.repo.t.ID.DESC())
	if err != nil || !slices.Equal(ids(all), []string{"b1", "a3", "a2", "a1"}) {
		t.Fatalf("findAll(nil): got %v, %v", ids(all), err)
	}
	empty, err := f.repo.findAll(ctx, w.ID("nope"))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("findAll of nothing: got %#v, %v; want an empty, non-nil slice", empty, err)
	}
	active, err := f.repo.ListActive(ctx, f.tenant)
	if err != nil || !slices.Equal(ids(active), []string{"a1", "a2"}) {
		t.Fatalf("ListActive: got %v, %v", ids(active), err)
	}
}

func TestExistsAndCount(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	for _, test := range []struct {
		cond   postgres.BoolExpression
		exists bool
		count  int64
	}{
		{w.TenantID(f.tenant), true, 3},
		{w.ID("nope"), false, 0},
		{nil, true, 4},
	} {
		exists, err := f.repo.exists(ctx, test.cond)
		if err != nil || exists != test.exists {
			t.Errorf("exists: got %v, %v; want %v", exists, err, test.exists)
		}
		count, err := f.repo.count(ctx, test.cond)
		if err != nil || count != test.count {
			t.Errorf("count: got %d, %v; want %d", count, err, test.count)
		}
	}
	if n, err := f.repo.CountInternal(ctx); err != nil || n != 4 {
		t.Errorf("CountInternal: got %d, %v", n, err)
	}
}

// deleteWhere and updateWhere return the rows they changed and refuse a nil
// condition.
func TestDeleteAndUpdateWhere(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	n, err := f.repo.updateWhere(ctx, w.TenantID(f.tenant).AND(w.Stock(0)), f.repo.t.Stock.SET(postgres.Int32(5)),
		f.repo.t.Active.SET(postgres.Bool(true)))
	if err != nil || n != 1 {
		t.Fatalf("updateWhere: got %d, %v; want 1", n, err)
	}
	if got, _ := f.repo.findOne(ctx, w.ID("a2")); got.Value().Stock != 5 {
		t.Fatalf("a2 stock after update = %d, want 5", got.Value().Stock)
	}
	if archived, archiveErr := f.repo.Archive(ctx, f.other, "a1"); archiveErr != nil || archived {
		t.Fatalf("Archive of another tenant's item: got %v, %v; want false", archived, archiveErr)
	}
	n, err = f.repo.deleteWhere(ctx, w.TenantID(f.tenant).AND(w.Status(model.ItemStatus_Active)))
	if err != nil || n != 2 {
		t.Fatalf("deleteWhere: got %d, %v; want 2", n, err)
	}
	if n, err = f.repo.deleteWhere(ctx, w.ID("nope")); err != nil || n != 0 {
		t.Fatalf("deleteWhere of nothing: got %d, %v", n, err)
	}
	if _, err = f.repo.deleteWhere(ctx, nil); err == nil {
		t.Fatal("deleteWhere(nil) ran")
	}
	if _, err = f.repo.updateWhere(ctx, nil, f.repo.t.Stock.SET(postgres.Int32(0))); err == nil {
		t.Fatal("updateWhere(nil) ran")
	}
	if count, _ := f.repo.count(ctx, nil); count != 2 {
		t.Fatalf("rows left = %d, want 2: a refused call changed rows", count)
	}
}

// queryRow maps the row an INSERT returns, and OnUnique translates the
// violation of the named key.
func TestQueryRowOnUnique(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.repo.Create(ctx, model.Items{ID: "dup", TenantID: f.tenant, Sku: "A-1", Name: "dup",
		Status: model.ItemStatus_Active, CreatedAt: f.created})
	if !errors.Is(err, ErrItemSKUExists) {
		t.Fatalf("err = %v, want ErrItemSKUExists", err)
	}
	event, err := NewAuditLog(f.db).Append(ctx, "a1", "created")
	if err != nil || event.ID == 0 || event.Action != "created" || event.ItemID == nil || *event.ItemID != "a1" {
		t.Fatalf("Append: got %+v, %v", event, err)
	}
}

// The helpers run in the transaction of the context: inside it they see its
// rows, and its rollback takes their writes.
func TestHelpersUseTheContextTransaction(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	rollback := errors.New("roll back")
	err := transactor.New(f.db).InTx(ctx, func(txCtx context.Context) error {
		if n, err := f.repo.deleteWhere(txCtx, w.TenantID(f.tenant)); err != nil || n != 3 {
			t.Fatalf("delete in tx: got %d, %v", n, err)
		}
		if n, err := f.repo.count(txCtx, w.TenantID(f.tenant)); err != nil || n != 0 {
			t.Fatalf("count in tx: got %d, %v", n, err)
		}
		if n, err := f.repo.count(ctx, w.TenantID(f.tenant)); err != nil || n != 3 {
			t.Fatalf("count outside tx: got %d, %v", n, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
	if n, countErr := f.repo.count(ctx, w.TenantID(f.tenant)); countErr != nil || n != 3 {
		t.Fatalf("count after rollback: got %d, %v", n, countErr)
	}
}

// lockable reports whether another transaction can lock row id of items
// right now. Any error but lock_not_available fails the test.
func lockable(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`SELECT id FROM items WHERE id = $1 FOR UPDATE NOWAIT`, id)
	var pqErr *pq.Error
	switch {
	case err == nil:
		return true
	case errors.As(err, &pqErr) && pqErr.Code == "55P03":
		return false
	default:
		t.Fatalf("probe: %v", err)
		return false
	}
}

// findOne, first and findAll honour a context row lock; PostgreSQL refuses
// it on exists and count, which aggregate; deleteWhere and updateWhere
// refuse it in the transactor, inside a transaction and outside one.
func TestRowLocks(t *testing.T) {
	f := newFixture(t)
	ctx, w := context.Background(), f.repo.where
	reads := map[string]func(context.Context) error{
		"findOne": func(c context.Context) error { _, err := f.repo.findOne(c, w.ID("a1")); return err },
		"first": func(c context.Context) error {
			_, err := f.repo.first(c, w.ID("a1"), f.repo.t.ID.ASC())
			return err
		},
		"findAll": func(c context.Context) error { _, err := f.repo.findAll(c, w.ID("a1")); return err },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			tx, err := f.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if err = read(transactor.ForUpdate(transactor.WithTx(ctx, tx))); err != nil {
				t.Fatalf("locked read: %v", err)
			}
			if lockable(t, f.db, "a1") {
				t.Fatal("the row is not locked")
			}
		})
	}
	for name, aggregate := range map[string]func(context.Context) error{
		"exists": func(c context.Context) error { _, err := f.repo.exists(c, w.ID("a1")); return err },
		"count":  func(c context.Context) error { _, err := f.repo.count(c, w.ID("a1")); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := transactor.New(f.db).InTx(ctx, func(txCtx context.Context) error {
				return aggregate(transactor.ForUpdate(txCtx))
			})
			var pqErr *pq.Error
			if !errors.As(err, &pqErr) || pqErr.Code != "0A000" {
				t.Fatalf("err = %v, want feature_not_supported (0A000): FOR UPDATE with an aggregate", err)
			}
		})
	}
	for name, write := range map[string]func(context.Context) error{
		"deleteWhere": func(c context.Context) error { _, err := f.repo.deleteWhere(c, w.ID("a1")); return err },
		"updateWhere": func(c context.Context) error {
			_, err := f.repo.updateWhere(c, w.ID("a1"), f.repo.t.Stock.SET(postgres.Int32(0)))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := write(transactor.ForUpdate(ctx)); !errors.Is(err, transactor.ErrRowLockOutsideTx) {
				t.Fatalf("outside a transaction: err = %v, want ErrRowLockOutsideTx", err)
			}
			err := transactor.New(f.db).InTx(ctx, func(txCtx context.Context) error {
				return write(transactor.ForUpdate(txCtx))
			})
			if !errors.Is(err, transactor.ErrRowLockNotSelect) {
				t.Fatalf("in a transaction: err = %v, want ErrRowLockNotSelect", err)
			}
		})
	}
	if n, _ := f.repo.count(ctx, nil); n != 4 {
		t.Fatalf("rows = %d, want 4: a refused write ran", n)
	}
}
