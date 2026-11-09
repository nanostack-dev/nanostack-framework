//go:build integration

package repotest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/pgerr"
)

// These tests attack the derived writes of notes.go across the two tenants
// of the fixture and two workspaces. Each one asserts what happened to the
// rows of the other tenant and workspace, read with plain SQL, not only
// what the method returned.

// noteFixture is the fixture with three notes: a1 and a2 of the tenant, in
// workspaces w1 and w2, and b1 of the other tenant, in w1.
type noteFixture struct {
	*fixture
	notes  NoteRepository
	writer NoteWriter
	w1, w2 context.Context //nolint:containedctx // the request contexts of two workspaces
	none   context.Context //nolint:containedctx // a context without a workspace
}

func newNoteFixture(t *testing.T) *noteFixture {
	t.Helper()
	f := &noteFixture{fixture: newFixture(t), none: context.Background()}
	f.notes, f.writer = NewNoteRepository(f.db), NewNoteWriter(f.db)
	f.w1, f.w2 = workspacescope.With(f.none, "w1"), workspacescope.With(f.none, "w2")
	for _, seed := range []struct {
		ctx    context.Context //nolint:containedctx // the workspace of the seed
		tenant uuid.UUID
		note   Note
	}{
		{f.w1, f.tenant, Note{ID: "a1", Slug: "alpha", Body: "a1 body"}},
		{f.w2, f.tenant, Note{ID: "a2", Slug: "beta", Body: "a2 body"}},
		{f.w1, f.other, Note{ID: "b1", Slug: "alpha", Body: "b1 body"}},
	} {
		if _, err := f.notes.CreateInTenantAndWorkspace(seed.ctx, seed.tenant, seed.note); err != nil {
			t.Fatalf("seed %s: %v", seed.note.ID, err)
		}
	}
	return f
}

// rows reads every note with plain SQL, one "id tenant workspace slug body
// pinned" line each, the tenants written as A and B.
func (f *noteFixture) rows(t *testing.T) string {
	t.Helper()
	res, err := f.db.QueryContext(context.Background(),
		`SELECT id, tenant_id, workspace_id, slug, body, pinned FROM notes ORDER BY id`)
	if err != nil {
		t.Fatalf("read notes: %v", err)
	}
	defer res.Close()
	var lines []string
	for res.Next() {
		var n model.Notes
		if err = res.Scan(&n.ID, &n.TenantID, &n.WorkspaceID, &n.Slug, &n.Body, &n.Pinned); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tenant := "?"
		switch n.TenantID {
		case f.tenant:
			tenant = "A"
		case f.other:
			tenant = "B"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s %q %t", n.ID, tenant, n.WorkspaceID, n.Slug, n.Body, n.Pinned))
	}
	if err = res.Err(); err != nil {
		t.Fatalf("read notes: %v", err)
	}
	return strings.Join(lines, "\n")
}

const seeded = `a1 A w1 alpha "a1 body" false
a2 A w2 beta "a2 body" false
b1 B w1 alpha "b1 body" false`

func (f *noteFixture) wantRows(t *testing.T, want string) {
	t.Helper()
	if got := f.rows(t); got != want {
		t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
	}
}

// A Create takes the tenant from its parameter and the workspace from the
// context: the tenant and workspace in the value are overwritten, so a value
// that names the other tenant still lands in the caller's. The database
// writes created_at and updated_at.
func TestCreateTakesTheCallersScope(t *testing.T) {
	f := newNoteFixture(t)
	got, err := f.notes.CreateInTenantAndWorkspace(f.w2, f.tenant,
		Note{ID: "a3", TenantID: f.other, WorkspaceID: "w1", Slug: "gamma", Body: "forged scope"})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != f.tenant || got.WorkspaceID != "w2" || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("created %+v, want the tenant, w2 and the database timestamps", got)
	}
	err = f.writer.CreateInTenantAndWorkspace(f.w1, f.other, Note{ID: "b2", TenantID: f.tenant, Slug: "delta",
		Body: "forged scope"})
	if err != nil {
		t.Fatal(err)
	}
	f.wantRows(t, seeded[:len(seeded)-len(`b1 B w1 alpha "b1 body" false`)]+
		`a3 A w2 gamma "forged scope" false
b1 B w1 alpha "b1 body" false
b2 B w1 delta "forged scope" false`)
}

// A request without a workspace cannot create a row: unlike a read, it has
// no tenant-wide fallback. Nothing is written.
func TestCreateWithoutWorkspaceFails(t *testing.T) {
	f := newNoteFixture(t)
	_, err := f.notes.CreateInTenantAndWorkspace(f.none, f.tenant, Note{ID: "a3", Slug: "gamma"})
	if !errors.Is(err, workspacescope.ErrNoWorkspace) {
		t.Fatalf("create without a workspace: %v, want ErrNoWorkspace", err)
	}
	if err = f.writer.CreateInTenantAndWorkspace(f.none, f.tenant, Note{ID: "a3", Slug: "gamma"}); !errors.Is(err,
		workspacescope.ErrNoWorkspace) {
		t.Fatalf("writer create without a workspace: %v, want ErrNoWorkspace", err)
	}
	f.wantRows(t, seeded)
}

// OnUnique maps a duplicate slug of the tenant, in any case, to
// ErrNoteSlugExists; the same slug in the other tenant is no conflict. The
// writer declares no OnUnique, so its duplicate keeps the PostgreSQL error.
func TestCreateMapsUniqueViolations(t *testing.T) {
	f := newNoteFixture(t)
	if _, err := f.notes.CreateInTenantAndWorkspace(f.w1, f.tenant, Note{ID: "a3", Slug: "ALPHA"}); !errors.Is(err,
		ErrNoteSlugExists) {
		t.Fatalf("a case-insensitive duplicate: %v, want ErrNoteSlugExists", err)
	}
	if _, err := f.notes.CreateInTenantAndWorkspace(f.w1, f.tenant, Note{ID: "a3", Slug: "alpha"}); !errors.Is(err,
		ErrNoteSlugExists) {
		t.Fatalf("a duplicate: %v, want ErrNoteSlugExists", err)
	}
	err := f.writer.CreateInTenantAndWorkspace(f.w1, f.tenant, Note{ID: "a3", Slug: "alpha"})
	if errors.Is(err, ErrNoteSlugExists) || !pgerr.IsUniqueViolation(err, "notes_tenant_slug_key") {
		t.Fatalf("a duplicate without OnUnique: %v, want the unique violation of notes_tenant_slug_key", err)
	}
	f.wantRows(t, seeded)
	if _, err = f.notes.CreateInTenantAndWorkspace(f.w1, f.other, Note{ID: "b2", Slug: "beta"}); err != nil {
		t.Fatalf("the slug of the tenant, in the other tenant: %v", err)
	}
}

// An update with the other tenant, or from another workspace, changes no
// row and returns sql.ErrNoRows (or an absent Option). The scope in the
// value is ignored: the row keeps its tenant and workspace.
func TestUpdateCannotCrossTenantOrWorkspace(t *testing.T) {
	f := newNoteFixture(t)
	attack := Note{ID: "a1", TenantID: f.other, WorkspaceID: "w1", Slug: "alpha", Body: "hijacked", Pinned: true}
	if _, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.w1, f.other, attack); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update with the other tenant: %v, want sql.ErrNoRows", err)
	}
	if _, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.w2, f.tenant, attack); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update from another workspace: %v, want sql.ErrNoRows", err)
	}
	if err := f.writer.UpdateByTenantAndWorkspaceAndID(f.w1, f.other, attack); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("writer update with the other tenant: %v, want sql.ErrNoRows", err)
	}
	if err := f.writer.UpdateByTenantAndWorkspaceAndID(f.w2, f.tenant, attack); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("writer update from another workspace: %v, want sql.ErrNoRows", err)
	}
	found, err := f.notes.UpdateByTenantAndWorkspaceAndSlug(f.w1, f.other,
		Note{Slug: "beta", Body: "hijacked"}) // beta is a2's slug, in the tenant
	if err != nil || found.IsPresent() {
		t.Fatalf("update by slug with the other tenant: %v, %v, want absent", found, err)
	}
	if found, err = f.notes.UpdateByTenantAndWorkspaceAndSlug(f.w1, f.tenant,
		Note{Slug: "beta", Body: "hijacked"}); err != nil || found.IsPresent() {
		t.Fatalf("update by slug from another workspace: %v, %v, want absent", found, err)
	}
	f.wantRows(t, seeded)

	got, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.w1, f.tenant, attack)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != f.tenant || got.WorkspaceID != "w1" || got.Body != "hijacked" {
		t.Fatalf("updated %+v, want the body changed in the tenant and w1", got)
	}
	f.wantRows(t, `a1 A w1 alpha "hijacked" true
a2 A w2 beta "a2 body" false
b1 B w1 alpha "b1 body" false`)
}

// A request without a workspace updates across the workspaces of its tenant,
// as a read does (workspacescope.Filter is TRUE), never in the other tenant.
func TestUpdateWithoutWorkspaceStaysInTheTenant(t *testing.T) {
	f := newNoteFixture(t)
	if _, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.none, f.tenant,
		Note{ID: "a2", Slug: "beta", Body: "tenant-wide"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.none, f.tenant,
		Note{ID: "b1", Slug: "alpha", Body: "other tenant"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update of the other tenant's row: %v, want sql.ErrNoRows", err)
	}
	f.wantRows(t, `a1 A w1 alpha "a1 body" false
a2 A w2 beta "tenant-wide" false
b1 B w1 alpha "b1 body" false`)
}

// A slug change that collides with another note of the tenant, in any case,
// is ErrNoteSlugExists, and changes nothing.
func TestUpdateMapsUniqueViolations(t *testing.T) {
	f := newNoteFixture(t)
	if _, err := f.notes.UpdateByTenantAndWorkspaceAndID(f.none, f.tenant,
		Note{ID: "a2", Slug: "Alpha", Body: "renamed"}); !errors.Is(err, ErrNoteSlugExists) {
		t.Fatalf("a colliding slug: %v, want ErrNoteSlugExists", err)
	}
	f.wantRows(t, seeded)
}

// The conflict key of an upsert holds the tenant: the other tenant's upsert
// of the same slug inserts its own row and leaves the tenant's alone. An
// upsert from another workspace of the tenant meets the row in w1 and does
// not update it (the DO UPDATE requires the same workspace): sql.ErrNoRows.
func TestUpsertCannotHijackAnotherScope(t *testing.T) {
	f := newNoteFixture(t)
	got, err := f.notes.UpsertByTenantAndWorkspaceAndSlug(f.w1, f.other,
		Note{ID: "b2", TenantID: f.tenant, Slug: "beta", Body: "other tenant's beta"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "b2" || got.TenantID != f.other {
		t.Fatalf("upserted %+v, want a new row b2 of the other tenant", got)
	}
	if _, err = f.notes.UpsertByTenantAndWorkspaceAndSlug(f.w2, f.tenant,
		Note{ID: "x", Slug: "alpha", Body: "hijacked"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("upsert onto a row of another workspace: %v, want sql.ErrNoRows", err)
	}
	if err = f.writer.UpsertByTenantAndWorkspaceAndSlug(f.w2, f.tenant,
		Note{ID: "x", Slug: "alpha", Body: "hijacked"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("writer upsert onto a row of another workspace: %v, want sql.ErrNoRows", err)
	}
	if _, err = f.notes.UpsertByTenantAndWorkspaceAndSlug(f.none, f.tenant,
		Note{ID: "x", Slug: "alpha", Body: "hijacked"}); !errors.Is(err, workspacescope.ErrNoWorkspace) {
		t.Fatalf("upsert without a workspace: %v, want ErrNoWorkspace", err)
	}
	f.wantRows(t, `a1 A w1 alpha "a1 body" false
a2 A w2 beta "a2 body" false
b1 B w1 alpha "b1 body" false
b2 B w1 beta "other tenant's beta" false`)

	// In its own scope, the upsert updates the row of the key: it keeps its
	// id, tenant and workspace, whatever the value says.
	got, err = f.notes.UpsertByTenantAndWorkspaceAndSlug(f.w1, f.tenant,
		Note{ID: "new", TenantID: f.other, WorkspaceID: "w2", Slug: "alpha", Body: "updated", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "a1" || got.TenantID != f.tenant || got.WorkspaceID != "w1" {
		t.Fatalf("upserted %+v, want a1 of the tenant in w1", got)
	}
	f.wantRows(t, `a1 A w1 alpha "updated" true
a2 A w2 beta "a2 body" false
b1 B w1 alpha "b1 body" false
b2 B w1 beta "other tenant's beta" false`)
}

// A plain Create on a table that no tenant column scopes takes the model
// itself (NoMapper) and reads the row back.
func TestCreateUnscopedModel(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	got, err := NewTenantRepository(f.db).Create(context.Background(), model.Tenants{ID: id, Name: "third"})
	if err != nil || got.ID != id || got.Name != "third" {
		t.Fatalf("create tenant: %+v, %v", got, err)
	}
}
