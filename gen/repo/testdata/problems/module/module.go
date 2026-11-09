// Package module declares a base in a module whose go.mod lacks a
// requirement that the base needs.
package module

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"

	"example.com/module/db/model"
	"example.com/module/db/table"
)

func _() {
	nanorepo.Base[model.AuditEvents](table.AuditEvents)
}
