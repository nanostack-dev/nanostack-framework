// Package viewbase binds a view. nano gen db writes no ColumnTypes on a
// view yet, so the base has no Contains or EqualsIgnoreCase predicate, and
// its doc comment says so.
package viewbase

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/viewbase/db/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/viewbase/db/view"
)

func _() {
	nanorepo.Base[model.AuditEvents](view.AuditEvents)
}
