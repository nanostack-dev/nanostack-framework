// Package searchforeignoption passes values that are not search options to
// Search: a type of another package with a method of the same name as the
// marker, and an operator. It must not compile.
package searchforeignoption

import "github.com/nanostack-dev/nanostack-framework/db/nanorepo"

type table struct{}

func (table) TableName() string { return "items" }

type forged struct{}

func (forged) searchOption() {}

func (forged) SearchOption() {}

func _() {
	nanorepo.Search[struct{}]("Item", table{}, forged{})
	nanorepo.Search[struct{}]("Item", table{}, nanorepo.Eq)
}
