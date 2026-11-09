package repotest

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// This file declares a repository whose only derived write is a create, in a
// file of its own: a create never meets no row, so its generated file must
// not import database/sql for sql.ErrNoRows.

// NoteCreator only inserts notes.
type NoteCreator interface {
	CreateInTenantAndWorkspace(ctx context.Context, tenantID uuid.UUID, note Note) (Note, error)
}

func _() {
	nanorepo.Implement[NoteCreator, noteCreatorImpl](table.Notes, noteToDomain, nanorepo.ToRow(noteToRow))
}

type noteCreatorImpl struct{ notesBase }

// NewNoteCreator returns the note creator on db.
func NewNoteCreator(db qrm.DB) NoteCreator {
	return &noteCreatorImpl{notesBase: newNotesBase(db)}
}
