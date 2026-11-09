// Package compile has a compile error of its own; nanostack gen repo reports it
// at its line and writes nothing.
package compile

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
)

func _() {
	nanorepo.Base[model.Tenants](table.Tenants)
}

type tenants struct{ tenantsBase }

func (r tenants) Find(ctx context.Context) (int64, error) {
	return r.count(ctx, r.where.Nmae("x"))
}

var _ = undefinedHelper()
