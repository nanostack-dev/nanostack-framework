package nanorepo_test

import (
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

// The declarations compile against a Nanostack DB table, infer the row and
// domain types from the mapper, and do nothing when called.
func TestDeclarationsCompileAndDoNothing(t *testing.T) {
	t.Parallel()
	items := postgres.NewTable("public", "items", "", postgres.StringColumn("id"))
	nanorepo.Implement[repository, repositoryImpl](items, toDomain)
	nanorepo.Implement[repository, repositoryImpl](items, nanorepo.NoMapper[row])
	nanorepo.Base[row](items)
	if got := nanorepo.NoMapper(row{ID: "a"}); got.ID != "a" {
		t.Fatalf("NoMapper(row) = %+v, want the row itself", got)
	}
}
