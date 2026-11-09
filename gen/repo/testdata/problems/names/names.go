// Package names declares names that the base of items would declare.
package names

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

type itemsBase struct{}

func newItemsBase() itemsBase { return itemsBase{} }

func _() {
	nanorepo.Base[model.Items](table.Items)
}
