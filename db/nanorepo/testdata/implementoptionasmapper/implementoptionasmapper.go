// Package implementoptionasmapper passes a search option to Implement in the
// place of the mapper. It must not compile.
package implementoptionasmapper

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
	nanorepo.Implement[repository, repositoryImpl](table{}, nanorepo.Sort(column{}))
}
