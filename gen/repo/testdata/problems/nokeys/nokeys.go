// Package nokeys binds a table whose package has no UniqueKeys (its
// ColumnTypes are hand-written for the test): a single-row find cannot be
// proven.
package nokeys

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/nokeys/db/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/nokeys/db/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// Events reads audit events.
type Events interface {
	FindByID(ctx context.Context, id int64) (functional.Option[model.AuditEvents], error)
	FindByAction(ctx context.Context, action string) ([]model.AuditEvents, error)
}

func _() {
	nanorepo.Implement[Events, eventsImpl](table.AuditEvents, nanorepo.NoMapper[model.AuditEvents])
}

type eventsImpl struct{ auditEventsBase }
