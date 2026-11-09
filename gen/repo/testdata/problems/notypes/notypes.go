// Package notypes binds a table whose package nanostack gen db wrote before it
// listed column types, and a table whose ColumnTypes misses a text column:
// nanostack gen repo cannot tell a text column from inet or jsonb.
package notypes

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/notypes/db/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/notypes/db/table"
)

func _() {
	nanorepo.Base[model.AuditEvents](table.AuditEvents)
	nanorepo.Base[model.Notes](table.Notes)
}
