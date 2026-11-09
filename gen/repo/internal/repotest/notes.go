package repotest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// This file declares the derived writes. notes has a hard tenant column
// (tenant_id), a NOT NULL context tenant column (workspace_id) and
// database-owned timestamps (Postgres.DatabaseColumns). Every write method
// below is generated, and the integration tests attack it across two
// tenants and two workspaces.

// ErrNoteSlugExists is returned when a tenant already has a note with the
// slug, in any case.
var ErrNoteSlugExists = errors.New("a note with this slug exists")

// Note is the domain type of a note. It carries its tenant and workspace,
// so a test can try to write another tenant's id through the value.
type Note struct {
	ID          string
	TenantID    uuid.UUID
	WorkspaceID string
	Slug        string
	Body        string
	Pinned      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func noteToDomain(row model.Notes) Note { return Note(row) }

func noteToRow(n Note) model.Notes { return model.Notes(n) }

// NoteRepository writes the notes of a tenant, reading the written row back.
type NoteRepository interface {
	CreateInTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID, note Note) (Note, error)
	UpdateByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, note Note) (Note, error)
	UpdateByTenantAndWorkspaceAndSlug(
		ctx context.Context, tenantID uuid.UUID, note Note,
	) (functional.Option[Note], error)
	UpsertByTenantAndWorkspaceAndSlug(ctx context.Context, tenantID uuid.UUID, note Note) (Note, error)
}

// NoteWriter is NoteRepository with the results that hold only an error,
// and without OnUnique: a duplicate slug keeps its PostgreSQL error.
type NoteWriter interface {
	CreateInTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID, note Note) error
	UpdateByTenantAndWorkspaceAndID(ctx context.Context, tenantID uuid.UUID, note Note) error
	UpsertByTenantAndWorkspaceAndSlug(ctx context.Context, tenantID uuid.UUID, note Note) error
}

// TenantRepository creates tenants, which no tenant column scopes: a plain
// Create of the model itself (NoMapper).
type TenantRepository interface {
	Create(ctx context.Context, tenant model.Tenants) (model.Tenants, error)
}

func _() {
	nanorepo.Implement[NoteRepository, noteRepositoryImpl](table.Notes, noteToDomain,
		nanorepo.ToRow(noteToRow),
		nanorepo.OnUnique(ErrNoteSlugExists, "notes_tenant_slug_key", "notes_tenant_lower_slug_idx"))
	nanorepo.Implement[NoteWriter, noteWriterImpl](table.Notes, noteToDomain, nanorepo.ToRow(noteToRow))
	nanorepo.Implement[TenantRepository, tenantRepositoryImpl](table.Tenants, nanorepo.NoMapper[model.Tenants])
}

type noteRepositoryImpl struct{ notesBase }

// NewNoteRepository returns the note repository on db.
func NewNoteRepository(db qrm.DB) NoteRepository {
	return &noteRepositoryImpl{notesBase: newNotesBase(db)}
}

type noteWriterImpl struct{ notesBase }

// NewNoteWriter returns the note writer on db.
func NewNoteWriter(db qrm.DB) NoteWriter {
	return &noteWriterImpl{notesBase: newNotesBase(db)}
}

type tenantRepositoryImpl struct{ tenantsBase }

// NewTenantRepository returns the tenant repository on db.
func NewTenantRepository(db qrm.DB) TenantRepository {
	return &tenantRepositoryImpl{tenantsBase: newTenantsBase(db)}
}
