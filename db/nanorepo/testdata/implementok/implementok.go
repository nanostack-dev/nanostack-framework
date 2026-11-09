// Package implementok declares a repository and a search correctly. It
// compiles, so the two other packages fail only on their one mistake.
package implementok

import "github.com/nanostack-dev/nanostack-framework/db/nanorepo"

type table struct{}

func (table) TableName() string { return "items" }

type column struct{}

func (column) Name() string { return "id" }

type row struct{ ID string }

type repository interface{ Find() row }

type repositoryImpl struct{}

func (repositoryImpl) Find() row { return row{} }

func toDomain(r row) row { return r }

func _() {
	nanorepo.Implement[repository, repositoryImpl](table{}, toDomain)
	nanorepo.Search[row]("Item", table{}, nanorepo.Sort(column{}))
}
