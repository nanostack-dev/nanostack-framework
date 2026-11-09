//go:build integration

package torture

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

func column(t *testing.T, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s.String)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// A table named group, with columns named by, and and count, an identity
// key and a nullable unique column: every shape of derived method runs, and
// no tenant column means no filter on a tenant.
func TestGroupsReservedWords(t *testing.T) {
	db := openTorture(t)
	ctx, repo := context.Background(), NewGroups(db)
	deleted := time.Date(2026, 5, 6, 7, 8, 9, 0, time.FixedZone("z", 2*3600))
	ids := map[string]int64{}
	for name, row := range map[string][]any{
		"g1": {"same", "k", "s1", 1, "b1", "a1", nil},
		"g2": {"same", nil, nil, 1, nil, nil, nil},
		"g3": {"other", nil, nil, 2, nil, nil, deleted},
		"g4": {"other", "k", nil, 3, "b1", "a2", nil},
	} {
		var id int64
		if err := db.QueryRow(`INSERT INTO "group" (name, name_key, slug, "count", "by", "and", deleted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, row...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	idsOf := func(rows []model.Group, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			for n, id := range ids {
				if id == r.ID {
					out = append(out, n)
				}
			}
		}
		slices.Sort(out)
		return out
	}
	eq := func(name string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	got, err := repo.FindByID(ctx, ids["g1"])
	if row, ok := got.Get(); err != nil || !ok || row.Slug == nil || *row.Slug != "s1" {
		t.Errorf("FindByID: %+v %v", got, err)
	}
	if g, ierr := repo.GetByID(ctx, ids["g2"]); ierr != nil || g.ID != ids["g2"] {
		t.Errorf("GetByID: %v %v", g, ierr)
	}
	if _, err = repo.GetByID(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByID of nothing: %v, want sql.ErrNoRows", err)
	}
	// The slug is unique and nullable: two NULL rows exist, a find by "s1" is one row, and no
	// value finds a NULL.
	if o, ierr := repo.FindBySlug(ctx, "s1"); ierr != nil || !o.IsPresent() {
		t.Errorf("FindBySlug s1: %v %v", o, ierr)
	}
	for _, v := range []string{"", "NULL", "null"} {
		if o, ierr := repo.FindBySlug(ctx, v); ierr != nil || o.IsPresent() {
			t.Errorf("FindBySlug %q matched a NULL slug: %v %v", v, o, ierr)
		}
		if ok, ierr := repo.ExistsBySlug(ctx, v); ierr != nil || ok {
			t.Errorf("ExistsBySlug %q: %v %v", v, ok, ierr)
		}
	}
	rows, err := repo.FindByName(ctx, "same")
	eq("FindByName", idsOf(rows, err), []string{"g1", "g2"})
	rows, err = repo.FindByNameAndNameKey(ctx, "other", "k")
	eq("FindByNameAndNameKey", idsOf(rows, err), []string{"g4"})
	// NameKey is a prefix of nothing, Name is a prefix of NameKey: both parse.
	rows, err = repo.FindByNameKey(ctx, "k")
	eq("FindByNameKey", idsOf(rows, err), []string{"g1", "g4"})
	rows, err = repo.FindByCount(ctx, 1)
	eq("FindByCount", idsOf(rows, err), []string{"g1", "g2"})
	rows, err = repo.FindByBy(ctx, "b1")
	eq("FindByBy", idsOf(rows, err), []string{"g1", "g4"})
	rows, err = repo.FindByAnd(ctx, "a1")
	eq("FindByAnd", idsOf(rows, err), []string{"g1"})
	rows, err = repo.FindByByAndAnd(ctx, "b1", "a2")
	eq("FindByByAndAnd", idsOf(rows, err), []string{"g4"})
	rows, err = repo.FindByIsInternal(ctx, false)
	eq("FindByIsInternal", idsOf(rows, err), []string{"g1", "g2", "g3", "g4"})
	// A soft-deleted row is a row: nothing filters deleted_at. The same instant in another zone finds it.
	rows, err = repo.FindByDeletedAt(ctx, deleted.UTC())
	eq("FindByDeletedAt", idsOf(rows, err), []string{"g3"})
	rows, err = repo.FindByNameInternal(ctx, "other")
	eq("FindByNameInternal", idsOf(rows, err), []string{"g3", "g4"})
	rows, err = repo.FindAll(ctx)
	eq("FindAll", idsOf(rows, err), []string{"g1", "g2", "g3", "g4"})
	rows, err = repo.FindAllInternal(ctx)
	eq("FindAllInternal", idsOf(rows, err), []string{"g1", "g2", "g3", "g4"})
	for name, count := range map[string]func() (int64, error){
		"Count": func() (int64, error) { return repo.Count(ctx) }, "CountInternal": func() (int64, error) { return repo.CountInternal(ctx) },
	} {
		if n, ierr := count(); ierr != nil || n != 4 {
			t.Errorf("%s: %d %v", name, n, ierr)
		}
	}
	if n, ierr := repo.CountByName(ctx, "same"); ierr != nil || n != 2 {
		t.Errorf("CountByName: %d %v", n, ierr)
	}
	if n, ierr := repo.CountByIsInternal(ctx, true); ierr != nil || n != 0 {
		t.Errorf("CountByIsInternal true: %d %v", n, ierr)
	}
	if ok, ierr := repo.ExistsByName(ctx, "other"); ierr != nil || !ok {
		t.Errorf("ExistsByName: %v %v", ok, ierr)
	}
	// Deletes remove exactly what the name says.
	if err = repo.DeleteByID(ctx, ids["g2"]); err != nil {
		t.Fatal(err)
	}
	if ok, ierr := repo.DeleteBySlug(ctx, "s1"); ierr != nil || !ok {
		t.Fatalf("DeleteBySlug: %v %v", ok, ierr)
	}
	if ok, ierr := repo.DeleteBySlug(ctx, "s1"); ierr != nil || ok {
		t.Fatalf("DeleteBySlug again: %v %v", ok, ierr)
	}
	if ok, ierr := repo.DeleteBySlug(ctx, ""); ierr != nil || ok {
		t.Fatalf("DeleteBySlug of the empty string deleted a NULL slug: %v %v", ok, ierr)
	}
	if n, ierr := repo.DeleteByNameAndNameKey(ctx, "other", "k"); ierr != nil || n != 1 {
		t.Fatalf("DeleteByNameAndNameKey: %d %v", n, ierr)
	}
	if n, ierr := repo.DeleteByName(ctx, "nobody"); ierr != nil || n != 0 {
		t.Fatalf("DeleteByName nobody: %d %v", n, ierr)
	}
	if left := column(t, db, `SELECT name FROM "group"`); !slices.Equal(left, []string{"other"}) {
		t.Fatalf("left %v, want only g3", left)
	}
}

// Every derived find returns a soft-deleted row: no derived method filters
// deleted_at. A service that soft-deletes names the filter by hand.
func TestSoftDeletedRowsAreReturned(t *testing.T) {
	db := openTorture(t)
	w := seedUsers(t, db)
	ctx, users := context.Background(), NewUsers(db)
	if !w.rows[3].deleted {
		t.Fatal("fixture: o0u4 is soft-deleted")
	}
	if o, err := users.FindByOrgAndID(ctx, orgs[0], idOf("o0u4")); err != nil || !o.IsPresent() {
		t.Errorf("FindByOrgAndID of a soft-deleted user: %v %v", o, err)
	}
	if n, err := users.CountByOrg(ctx, orgs[0]); err != nil || n != 4 {
		t.Errorf("CountByOrg counts the soft-deleted user: %d %v", n, err)
	}
	// The soft-deleted u4 and the live u1 share handle h1: the partial unique index allows it, and a
	// derived find on the handle returns both.
	rows, err := users.FindByOrgAndHandle(ctx, orgs[0], "h1")
	if err != nil || len(rows) != 2 {
		t.Errorf("FindByOrgAndHandle h1: %d rows, %v; want the live and the soft-deleted row", len(rows), err)
	}
	if n, ierr := users.DeleteByOrgAndName(ctx, orgs[0], "other"); ierr != nil || n != 2 {
		t.Errorf("a derived delete removes soft-deleted rows too: %d %v", n, ierr)
	}
	want := []string{"o0u1", "o0u2", "o1u1", "o1u2", "o1u3", "o1u4"}
	if left := column(t, db, `SELECT "MixedCase" FROM "user"`); !slices.Equal(left, want) {
		t.Errorf("left %v, want %v: the other org keeps its rows", left, want)
	}
}

// A partial and an expression unique index are not keys: nanostack gen db
// leaves them out of UniqueKeys, so no single-row find derives on them
// (the golden testdata/torture/keys shows the refusals), and a single-row read on a
// handle would fail on the duplicate that the index allows.
func TestPartialAndExpressionIndexesAreNotKeys(t *testing.T) {
	db := openTorture(t)
	seedUsers(t, db)
	keys := table.User.UniqueKeys()
	for name, cols := range keys {
		if slices.Contains(cols, "handle") || slices.Contains(cols, "email") {
			t.Errorf("UniqueKeys lists %s %v", name, cols)
		}
	}
	users, ok := NewUsers(db).(*usersImpl)
	if !ok {
		t.Fatal("NewUsers does not return *usersImpl")
	}
	_, err := users.findOne(context.Background(), users.where.OrgID(orgs[0]).AND(users.where.Handle("h1")))
	if !errors.Is(err, transactor.ErrMoreThanOneRow) {
		t.Errorf("findOne on the partial index columns: %v, want ErrMoreThanOneRow", err)
	}
	_, err = db.Exec(
		`INSERT INTO "user" (id, org_id, email, handle, name, name_key) VALUES ($1,$2,'E1@X','hz','n','z')`,
		uuid.New(),
		orgs[0],
	)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
		t.Errorf("a second email that differs by case: %v, want a unique violation", err)
	}
}

// A unique key on a nullable column: NULLs are distinct, so many rows have
// no value. A derived find by value returns the one row with that value, and
// no value finds, counts or deletes the NULL rows.
func TestNullableUniqueKey(t *testing.T) {
	db := openTorture(t)
	seedUsers(t, db)
	ctx, users := context.Background(), NewUsers(db)
	if n := column(
		t,
		db,
		`SELECT external_id FROM "user" WHERE org_id=$1 AND external_id IS NULL`,
		orgs[0],
	); len(
		n,
	) != 2 {
		t.Fatalf("fixture: two users of org 0 have a NULL external_id, got %d", len(n))
	}
	if o, err := users.FindByOrgAndExternalID(ctx, orgs[0], "x1"); err != nil || !o.IsPresent() {
		t.Errorf("x1: %v %v", o, err)
	}
	for _, v := range []string{"", "NULL", "null", "x3"} {
		if o, err := users.FindByOrgAndExternalID(ctx, orgs[0], v); err != nil || o.IsPresent() {
			t.Errorf("FindByOrgAndExternalID %q found %v, %v: it must not match a NULL external_id", v, o, err)
		}
		if ok, err := users.DeleteByOrgAndExternalID(ctx, orgs[0], v); err != nil || ok {
			t.Errorf("DeleteByOrgAndExternalID %q deleted: %v %v", v, ok, err)
		}
	}
	if n := column(t, db, `SELECT 1 FROM "user"`); len(n) != 8 {
		t.Errorf("%d users left, want 8", len(n))
	}
}

// A key that a migration dropped cannot return a silent first row.
func TestDroppedKeyIsAnError(t *testing.T) {
	db := openTorture(t)
	seedUsers(t, db)
	ctx, users := context.Background(), NewUsers(db)
	mustExec(t, db, `ALTER TABLE "user" DROP CONSTRAINT user_org_id_name_name_key_key`)
	mustExec(
		t,
		db,
		`INSERT INTO "user" (id, org_id, email, handle, name, name_key) VALUES ($1,$2,'twin@x','twin','same','k1')`,
		uuid.New(),
		orgs[0],
	)
	_, dupErr := users.FindByOrgAndNameAndNameKey(ctx, orgs[0], "same", "k1")
	if !errors.Is(dupErr, transactor.ErrMoreThanOneRow) {
		t.Errorf("err = %v, want ErrMoreThanOneRow", dupErr)
	}
	if _, pkErr := users.GetByOrgAndID(ctx, orgs[0], idOf("o0u1")); pkErr != nil {
		t.Errorf("a find on the primary key still works: %v", pkErr)
	}
}

// A single-row find on an order whose key a migration dropped is an error on
// a second row, with a context scope in the name too.
func TestDroppedOrderKeyIsAnError(t *testing.T) {
	db := openTorture(t)
	seedOrders(t, db, false)
	orders := NewOrders(db)
	mustExec(t, db, `ALTER TABLE "order" DROP CONSTRAINT order_org_id_product_id_by_key`)
	mustExec(t, db, `INSERT INTO "order" (id, org_id, product_id, workspace_id, "by", "and", order_by, brand_and_model,
		id_and_name, name, name_key) VALUES ($1,$2,$3,'w1','by-w1-1','a','o','b','i','n','twin')`,
		uuid.New(), orgs[0], products[0])
	_, err := orders.FindByOrgAndProductAndWorkspaceAndBy(workspacescopeCtx("w1"), orgs[0], products[0], "by-w1-1")
	if !errors.Is(err, transactor.ErrMoreThanOneRow) {
		t.Errorf("err = %v, want ErrMoreThanOneRow", err)
	}
	// Another product has one row of that value: still one.
	if o, oErr := orders.FindByOrgAndProductAndWorkspaceAndBy(
		workspacescopeCtx("w1"),
		orgs[0],
		products[1],
		"by-w1-1",
	); oErr != nil ||
		!o.IsPresent() {
		t.Errorf("other product: %v %v", o, oErr)
	}
}

func workspacescopeCtx(ws string) context.Context { return ctxOf(ws) }

// A table with a tenant column and no key: finds return every matching row,
// and a delete removes only its tenant's rows.
func TestLogLinesWithoutAKey(t *testing.T) {
	db := openTorture(t)
	ctx, lines := context.Background(), NewLogLines(db)
	for _, org := range []uuid.UUID{orgs[0], orgs[0], orgs[0], orgs[1]} {
		mustExec(t, db, `INSERT INTO log_line (org_id, msg) VALUES ($1,'x')`, org)
	}
	if rows, err := lines.FindByOrgAndMsg(ctx, orgs[0], "x"); err != nil || len(rows) != 3 {
		t.Errorf("FindByOrgAndMsg: %d %v", len(rows), err)
	}
	if n, err := lines.CountByOrg(ctx, orgs[1]); err != nil || n != 1 {
		t.Errorf("CountByOrg: %d %v", n, err)
	}
	if ok, err := lines.ExistsByOrgAndMsg(ctx, orgs[2], "x"); err != nil || ok {
		t.Errorf("ExistsByOrgAndMsg of an org with no line: %v %v", ok, err)
	}
	if n, err := lines.DeleteByOrgAndMsg(ctx, orgs[0], "x"); err != nil || n != 3 {
		t.Errorf("DeleteByOrgAndMsg: %d %v", n, err)
	}
	if left := column(t, db, `SELECT org_id::text FROM log_line`); !slices.Equal(left, []string{orgs[1].String()}) {
		t.Errorf("left %v, want org 1's line", left)
	}
}
