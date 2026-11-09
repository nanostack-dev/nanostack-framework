//go:build integration

package torture

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
)

// seedOrders seeds orders for 2 orgs x 2 products x workspaces w1, w2 and
// NULL, two or three rows each. Every tenant has the same values; the label
// of a row is its name_key.
func seedOrders(t *testing.T, db execer, reverse bool) world {
	t.Helper()
	w := world{table: `"order"`, label: "name_key"}
	var inserts []func()
	for o := range 2 {
		for p := range 2 {
			for _, ws := range []string{"w1", "w2", ""} {
				ks := []int{1, 2}
				if ws == "" {
					ks = append(ks, 3)
				}
				for _, k := range ks {
					wsLabel := cmpOr(ws, "null")
					r := trow{
						label: fmt.Sprintf(
							"o%dp%d%sk%d",
							o,
							p,
							wsLabel,
							k,
						),
						org:     o,
						product: p,
						ws:      ws,
						wsNull:  ws == "",
						name:    "same",
						by:      fmt.Sprintf("by-%s-%d", wsLabel, k),
						count:   int32(k),
						flag:    k == 1,
						flag2:   k == 2,
						text:    fmt.Sprintf("bm%d", k),
						text2:   fmt.Sprintf("in%d", k),
						text3:   fmt.Sprintf("ob%d", k),
						memo:    fmt.Sprintf("and%d", k),
						deleted: k == 2,
					}
					r.nameKey = r.label
					if k == 2 {
						r.name = "other"
					}
					var wsArg, deleted any
					if ws != "" {
						wsArg = ws
					}
					if r.deleted {
						deleted = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
					}
					inserts = append(inserts, func() {
						mustExec(
							t,
							db,
							`INSERT INTO "order" (id, org_id, product_id, workspace_id, "by", "and", "count", internal,
						order_by, is_internal, brand_and_model, id_and_name, name, name_key, deleted_at)
						VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
							idOf(
								r.label,
							),
							orgs[o],
							products[p],
							wsArg,
							r.by,
							r.memo,
							r.count,
							r.flag,
							r.text3,
							r.flag2,
							r.text,
							r.text2,
							r.name,
							r.nameKey,
							deleted,
						)
					})
					w.rows = append(w.rows, r)
				}
			}
		}
	}
	if reverse {
		slices.Reverse(inserts)
	}
	for _, insert := range inserts {
		insert()
	}
	return w
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orderLabel(o model.Order) string { return o.NameKey }

// The rows are inserted in two orders: no derived method depends on the
// physical order of the table.
func TestOrdersIsolation(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) { testOrdersIsolation(t, reverse) })
	}
}

func testOrdersIsolation(t *testing.T, reverse bool) {
	db := openTorture(t)
	w := seedOrders(t, db, reverse)
	repo := NewOrders(db)
	labelVals := vals("o0p0w1k1", "o1p1w2k2", "o0p1nullk3", "o1p0nullk1", "missing")
	byVals := vals("by-w1-1", "by-w2-2", "by-null-1", "by-null-3", "missing")
	nameVals := vals("same", "other", "missing")
	id := func(a args) uuid.UUID { return idOf(a.s) }
	o := func(a args) uuid.UUID { return orgs[a.org] }
	p := func(a args) uuid.UUID { return products[a.product] }
	by := func(r trow, a args) bool { return r.by == a.s }
	name := func(r trow, a args) bool { return r.name == a.s }
	label := func(r trow, a args) bool { return r.label == a.s }
	lbl := labs(orderLabel)
	optLbl := opt(orderLabel)
	both := scOrg | scProduct
	pairVals := []args{{s: "same", s2: "o0p0w1k1"}, {s: "other", s2: "o0p0w1k1"}, {s: "same", s2: "o1p1w2k1"},
		{s: "other", s2: "o1p1w2k2"}, {s: "same", s2: "missing"}}
	specs := []spec{
		{name: "FindByOrgAndProductAndWorkspaceAndID", sc: both, ws: wsCtx, vals: labelVals, pred: label, single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndProductAndWorkspaceAndID(c, o(a), p(a), id(a)))
			}},
		{
			name:   "GetByOrgAndProductAndWorkspaceAndID",
			sc:     both,
			ws:     wsCtx,
			vals:   labelVals,
			pred:   label,
			single: true,
			get:    true,
			find: func(c context.Context, a args) ([]string, error) {
				return one(orderLabel)(repo.GetByOrgAndProductAndWorkspaceAndID(c, o(a), p(a), id(a)))
			},
		},
		{name: "FindByOrgAndProductAndWorkspaceAndBy", sc: both, ws: wsCtx, vals: byVals, pred: by, single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndProductAndWorkspaceAndBy(c, o(a), p(a), a.s))
			}},
		{
			name:   "FindByOrgAndProductAndWorkspaceIDAndName",
			sc:     both,
			ws:     wsParam,
			vals:   nameVals,
			pred:   name,
			single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndProductAndWorkspaceIDAndName(c, o(a), p(a), a.ws, a.s))
			},
		},
		{name: "FindByOrgIDAndProductIDAndWorkspaceAndBy", sc: both, ws: wsCtx, vals: byVals, pred: by, single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgIDAndProductIDAndWorkspaceAndBy(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndName", sc: both, ws: wsCtx, vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndName(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndNameAndNameKey", sc: both, ws: wsCtx, vals: pairVals,
			pred: func(r trow, a args) bool { return r.name == a.s && r.nameKey == a.s2 },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndNameAndNameKey(c, o(a), p(a), a.s, a.s2))
			}},
		{
			name: "FindByOrgAndProductAndWorkspaceAndBrandAndModel",
			sc:   both,
			ws:   wsCtx,
			vals: vals("bm1", "bm2", "bm3", "missing"),
			pred: func(r trow, a args) bool { return r.text == a.s },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndBrandAndModel(c, o(a), p(a), a.s))
			},
		},
		// IDAndName is one column, not ID and Name: one parameter.
		{name: "FindByOrgAndProductAndWorkspaceAndIDAndName", sc: both, ws: wsCtx, vals: vals("in1", "in2", "missing"),
			pred: func(r trow, a args) bool { return r.text2 == a.s },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndIDAndName(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndCount", sc: both, ws: wsCtx,
			vals: []args{{n: 1}, {n: 2}, {n: 3}, {n: 9}},
			pred: func(r trow, a args) bool { return int64(r.count) == a.n },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndCount(c, o(a), p(a), int32(a.n)))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndAnd", sc: both, ws: wsCtx, vals: vals("and1", "and3", "missing"),
			pred: func(r trow, a args) bool { return r.memo == a.s },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndAnd(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndInternal", sc: both, ws: wsCtx, vals: []args{{b: true}, {b: false}},
			pred: func(r trow, a args) bool { return r.flag == a.b },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndInternal(c, o(a), p(a), a.b))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndIsInternal", sc: both, ws: wsCtx, vals: []args{{b: true}, {b: false}},
			pred: func(r trow, a args) bool { return r.flag2 == a.b },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndIsInternal(c, o(a), p(a), a.b))
			}},
		{name: "FindByOrgAndProductAndWorkspaceAndOrderBy", sc: both, ws: wsCtx, vals: vals("ob1", "ob2", "missing"),
			pred: func(r trow, a args) bool { return r.text3 == a.s },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspaceAndOrderBy(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndProductAndWorkspace", sc: both, ws: wsCtx,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndWorkspace(c, o(a), p(a)))
			}},
		{name: "FindByOrgAndWorkspaceAndProduct", sc: both, ws: wsCtx,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndWorkspaceAndProduct(c, o(a), p(a)))
			}},
		{name: "FindByOrgAndProductIDAndWorkspaceIDAndName", sc: both, ws: wsParam, vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductIDAndWorkspaceIDAndName(c, o(a), p(a), a.ws, a.s))
			}},
		{name: "FindByOrgAndProductAndNameInternal", sc: both, vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndNameInternal(c, o(a), p(a), a.s))
			}},
		{name: "FindByOrgAndNameInternal", sc: scOrg, vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndNameInternal(c, o(a), a.s))
			}},
		{
			name:  "CountByOrgAndNameInternal",
			sc:    scOrg,
			vals:  nameVals,
			pred:  name,
			count: func(c context.Context, a args) (int64, error) { return repo.CountByOrgAndNameInternal(c, o(a), a.s) },
		},
		{
			name:   "ExistsByOrgAndNameInternal",
			sc:     scOrg,
			vals:   nameVals,
			pred:   name,
			exists: func(c context.Context, a args) (bool, error) { return repo.ExistsByOrgAndNameInternal(c, o(a), a.s) },
		},
		{name: "FindByNameInternal", vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByNameInternal(c, a.s)) }},
		{name: "FindByIDInternal", vals: labelVals, pred: label, single: true,
			find: func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByIDInternal(c, id(a))) }},
		{name: "FindAllInternal",
			find: func(c context.Context, _ args) ([]string, error) { return lbl(repo.FindAllInternal(c)) }},
		{name: "CountInternal", count: func(c context.Context, _ args) (int64, error) { return repo.CountInternal(c) }},
		{name: "CountByOrgAndProductAndWorkspace", sc: both, ws: wsCtx,
			count: func(c context.Context, a args) (int64, error) {
				return repo.CountByOrgAndProductAndWorkspace(c, o(a), p(a))
			}},
		{name: "CountByOrgAndProductAndWorkspaceAndName", sc: both, ws: wsCtx, vals: nameVals, pred: name,
			count: func(c context.Context, a args) (int64, error) {
				return repo.CountByOrgAndProductAndWorkspaceAndName(c, o(a), p(a), a.s)
			}},
		{name: "ExistsByOrgAndProductAndWorkspaceAndName", sc: both, ws: wsCtx, vals: nameVals, pred: name,
			exists: func(c context.Context, a args) (bool, error) {
				return repo.ExistsByOrgAndProductAndWorkspaceAndName(c, o(a), p(a), a.s)
			}},
		{name: "ExistsByOrgAndProductAndWorkspaceAndID", sc: both, ws: wsCtx, vals: labelVals, pred: label,
			exists: func(c context.Context, a args) (bool, error) {
				return repo.ExistsByOrgAndProductAndWorkspaceAndID(c, o(a), p(a), id(a))
			}},
		{name: "DeleteByOrgAndProductAndWorkspaceAndID", sc: both, ws: wsCtx, vals: labelVals, pred: label,
			del: func(c context.Context, a args) (int64, error) {
				return affected(repo.DeleteByOrgAndProductAndWorkspaceAndID(c, o(a), p(a), id(a)))
			}},
		{name: "DeleteByOrgAndProductAndWorkspaceAndBy", sc: both, ws: wsCtx, vals: byVals, pred: by,
			del: func(c context.Context, a args) (int64, error) {
				return none(repo.DeleteByOrgAndProductAndWorkspaceAndBy(c, o(a), p(a), a.s))
			}},
		{name: "DeleteByOrgAndProductAndWorkspaceAndName", sc: both, ws: wsCtx, vals: nameVals, pred: name,
			del: func(c context.Context, a args) (int64, error) {
				return repo.DeleteByOrgAndProductAndWorkspaceAndName(c, o(a), p(a), a.s)
			}},
		{name: "DeleteByOrgAndProductAndWorkspaceIDAndName", sc: both, ws: wsParam, vals: nameVals, pred: name,
			del: func(c context.Context, a args) (int64, error) {
				return repo.DeleteByOrgAndProductAndWorkspaceIDAndName(c, o(a), p(a), a.ws, a.s)
			}},
		{name: "DeleteByOrgAndProductAndWorkspace", sc: both, ws: wsCtx,
			del: func(c context.Context, a args) (int64, error) {
				return repo.DeleteByOrgAndProductAndWorkspace(c, o(a), p(a))
			}},
	}
	runSpecs(t, db, w, specs)
}
