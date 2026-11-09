// Package declarations holds one wrong declaration per rule; nanostack gen repo
// reports every one of them in one run.
package declarations

import (
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/mappers"
)

// Repo is the repository interface of every declaration below.
type Repo interface{ Ping() }

type (
	itemsImpl          struct{}
	otherImpl          struct{}
	statefulImpl       struct{}
	foreignImpl        struct{}
	exprImpl           struct{}
	madeImpl           struct{}
	tenantsImpl        struct{}
	notIfaceImpl       struct{}
	aliasImpl          struct{}
	mysqlImpl          struct{}
	fakeImpl           struct{}
	auditImpl          struct{}
	badCallImpl        struct{}
	genericImpl[T any] struct{ v T }
)

// cipherMapper has state: its zero value cannot decrypt.
type cipherMapper struct{ key string }

func (m *cipherMapper) ToDomain(row model.Items) mappers.Item {
	return mappers.Item{ID: row.ID + m.key}
}

var (
	itemToDomain     = mappers.ItemToDomain
	statefulToDomain = (&cipherMapper{key: "k"}).ToDomain
	madeToDomain     = makeMapper()
)

func makeMapper() func(model.Items) mappers.Item { return mappers.ItemToDomain }

// mysqlItemsTable is a MySQL table.
type mysqlItemsTable struct {
	mysql.Table

	ID mysql.ColumnString
}

var mysqlItems = &mysqlItemsTable{}

// fakeTable has a table name and nothing else.
type fakeTable struct{}

func (fakeTable) TableName() string { return "fake" }

var notATable = &fakeTable{}

func _() {
	nanorepo.Implement[Repo, itemsImpl](table.Items, itemToDomain)
	nanorepo.Implement[Repo, otherImpl](table.Items, mappers.ItemToDomain)
	nanorepo.Implement[Repo, statefulImpl](table.Items, statefulToDomain)
	nanorepo.Implement[Repo, foreignImpl](table.Items, mappers.ItemToDomainVar)
	nanorepo.Implement[Repo, exprImpl](table.Items, (&cipherMapper{}).ToDomain)
	nanorepo.Implement[Repo, madeImpl](table.Items, madeToDomain)
	nanorepo.Implement[Repo, itemsImpl](table.Items, itemToDomain)

	nanorepo.Base[model.Tenants](table.Tenants)
	nanorepo.Implement[Repo, tenantsImpl](table.Tenants, nanorepo.NoMapper[model.Tenants])
	nanorepo.Base[model.Items](table.Tenants)
	nanorepo.Implement[Repo, genericImpl[string]](table.Tenants, nanorepo.NoMapper[model.Tenants])
	nanorepo.Implement[model.Items, notIfaceImpl](table.Tenants, nanorepo.NoMapper[model.Tenants])
	nanorepo.Implement[Repo, mappers.Item](table.Tenants, nanorepo.NoMapper[model.Tenants])

	nanorepo.Implement[Repo, aliasImpl](table.Items.AS("i"), itemToDomain)
	nanorepo.Base[model.Items](mysqlItems)
	nanorepo.Base[model.Items](notATable)
	nanorepo.Base[model.Tenants](table.AuditEvents)
	nanorepo.Implement[Repo, badCallImpl](table.AuditEvents, 42)
}

var _ = auditImpl{}
var _ = mysqlImpl{}
var _ = fakeImpl{}
