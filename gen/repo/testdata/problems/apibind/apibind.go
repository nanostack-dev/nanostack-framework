// Package apibind declares searches whose request bodies disagree with
// them: each API binding rule (A1 to A10) reports once, and E2 refuses
// InFold on a uuid column.
package apibind

import (
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/model"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/db/shop/public/table"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/problems/apibind/apigen"
)

func _() {
	nanorepo.Base[model.Items](table.Items)

	// A1 (ip_address), A2 (created_at), A9 (the page), A10 (extra).
	nanorepo.Search[model.Items]("BadSort", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name, table.Items.CreatedAt),
		nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc),
		nanorepo.API[apigen.BadSortRequest](),
		nanorepo.IgnoresFullText("the product decides"),
	)

	// A3 (ip_addresses), A4 (signature_status), A5, A9 (ids maxItems).
	nanorepo.Search[model.Items]("BadFilter", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.ID, nanorepo.In),
		nanorepo.Filter(table.Items.Name, nanorepo.Eq.API("signature_status")),
		nanorepo.Sort(table.Items.Name),
		nanorepo.DefaultSort(table.Items.Name, nanorepo.Asc),
		nanorepo.API[apigen.BadFilterRequest](),
	)

	// IgnoresFullText on a full_text_search of the endpoint's own schema;
	// A8: no operation takes the body.
	nanorepo.Search[model.Items]("OwnText", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Sort(table.Items.Name),
		nanorepo.API[apigen.OwnTextRequest](),
		nanorepo.IgnoresFullText("the product decides"),
	)

	// A6: FullText, and the body has no full_text_search.
	nanorepo.Search[model.Items]("NoText", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.FullText(table.Items.Name),
		nanorepo.Sort(table.Items.Name),
		nanorepo.API[apigen.NoTextRequest](),
	)

	// E2: InFold on a uuid column.
	nanorepo.Search[model.Items]("Folded", table.Items,
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.TenantID, nanorepo.InFold),
		nanorepo.Sort(table.Items.Name),
	)
}
