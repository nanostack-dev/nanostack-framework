// Package target has a hand-written file named like the derived file of
// target.go: nanostack gen repo never overwrites a file it did not write.
package target

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

// Events appends audit events.
type Events interface {
	CountByAction(ctx context.Context, action string) (int64, error)
}

func _() {
	nanorepo.Implement[Events, eventsImpl](table.AuditEvents, nanorepo.NoMapper[model.AuditEvents])
}

type eventsImpl struct{ auditEventsBase }
