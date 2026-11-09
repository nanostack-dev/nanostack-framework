package nanorepo_test

import (
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
)

type row struct{ ID string }

type domain struct{ ID string }

type repository interface{ Find() domain }

type repositoryImpl struct{}

func (repositoryImpl) Find() domain { return domain{} }

func toDomain(r row) domain { return domain(r) }

func toRow(d domain) row { return row(d) }

var errExists = errors.New("item exists")

// The declarations compile against a Nanostack DB table, infer the row and
// domain types from the mapper, and do nothing when called.
func TestDeclarationsCompileAndDoNothing(t *testing.T) {
	t.Parallel()
	items := postgres.NewTable("public", "items", "", postgres.StringColumn("id"))
	nanorepo.Implement[repository, repositoryImpl](items, toDomain)
	nanorepo.Implement[repository, repositoryImpl](items, nanorepo.NoMapper[row])
	nanorepo.Implement[repository, repositoryImpl](items, toDomain,
		nanorepo.ToRow(toRow), nanorepo.OnUnique(errExists, "items_pkey"))
	nanorepo.Base[row](items)
	if got := nanorepo.NoMapper(row{ID: "a"}); got.ID != "a" {
		t.Fatalf("NoMapper(row) = %+v, want the row itself", got)
	}
}
