//go:build integration

package torture

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
)

// seedUsers seeds four users in each of two orgs, with the same values in
// both. The label of a row is its "MixedCase" column.
func seedUsers(t *testing.T, db execer) world {
	t.Helper()
	w := world{table: `"user"`, label: `"MixedCase"`}
	for o := range 2 {
		for k := 1; k <= 4; k++ {
			r := trow{
				label: fmt.Sprintf("o%du%d", o, k), org: o, name: "same", nameKey: fmt.Sprintf("k%d", (k-1)%2+1),
				handle: "h1", email: fmt.Sprintf("e%d@x", k), external: fmt.Sprintf("x%d", k), flag: k == 1,
				deleted: k == 4,
			}
			if k > 2 {
				r.name, r.externalNull = "other", true
			}
			if k == 4 {
				r.handle = "h1" // shares the handle of u1, which is live: the partial index allows it
			} else {
				r.handle = fmt.Sprintf("h%d", k)
			}
			if k == 1 {
				r.handle = "h1"
			}
			var ext, deleted any
			if !r.externalNull {
				ext = r.external
			}
			if r.deleted {
				deleted = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			email := r.email
			if k == 2 {
				email = "E2@X" // the unique index is on lower(email)
				r.email = email
			}
			mustExec(
				t,
				db,
				`INSERT INTO "user" (id, org_id, email, handle, external_id, name, name_key, internal, "MixedCase", deleted_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				idOf(r.label),
				orgs[o],
				email,
				r.handle,
				ext,
				r.name,
				r.nameKey,
				r.flag,
				r.label,
				deleted,
			)
			w.rows = append(w.rows, r)
		}
	}
	return w
}

func userLabel(u model.User) string { return *u.MixedCase }

func TestUsersIsolation(t *testing.T) {
	db := openTorture(t)
	w := seedUsers(t, db)
	repo := NewUsers(db)
	labelVals := vals("o0u1", "o1u2", "o0u3", "o1u4", "missing")
	nameVals := vals("same", "other", "missing")
	id := func(a args) uuid.UUID { return idOf(a.s) }
	o := func(a args) uuid.UUID { return orgs[a.org] }
	label := func(r trow, a args) bool { return r.label == a.s }
	name := func(r trow, a args) bool { return r.name == a.s }
	lbl, optLbl := labs(userLabel), opt(userLabel)
	org := scOrg
	ext := func(r trow, a args) bool { return !r.externalNull && r.external == a.s }
	specs := []spec{
		{
			name:   "FindByOrgAndID",
			sc:     org,
			vals:   labelVals,
			pred:   label,
			single: true,
			find:   func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByOrgAndID(c, o(a), id(a))) },
		},
		{name: "GetByOrgAndID", sc: org, vals: labelVals, pred: label, single: true, get: true,
			find: func(c context.Context, a args) ([]string, error) {
				return one(userLabel)(repo.GetByOrgAndID(c, o(a), id(a)))
			}},
		{name: "GetByOrgIDAndID", sc: org, vals: labelVals, pred: label, single: true, get: true,
			find: func(c context.Context, a args) ([]string, error) {
				return one(userLabel)(repo.GetByOrgIDAndID(c, OrgID(o(a)), id(a)))
			}},
		{
			name:   "FindByOrgAndExternalID",
			sc:     org,
			vals:   vals("x1", "x2", "x3", "missing"),
			pred:   ext,
			single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndExternalID(c, o(a), a.s))
			},
		},
		{
			name:   "FindByOrgAndNameAndNameKey",
			sc:     org,
			single: true,
			vals: []args{
				{s: "same", s2: "k1"},
				{s: "same", s2: "k2"},
				{s: "other", s2: "k1"},
				{s: "other", s2: "k2"},
				{s: "other", s2: "k3"},
			},
			pred: func(r trow, a args) bool { return r.name == a.s && r.nameKey == a.s2 },
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndNameAndNameKey(c, o(a), a.s, a.s2))
			},
		},
		{
			name: "FindByOrgAndName",
			sc:   org,
			vals: nameVals,
			pred: name,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByOrgAndName(c, o(a), a.s)) },
		},
		{name: "FindByOrgAndHandle", sc: org, vals: vals("h1", "h2", "missing"),
			pred: func(r trow, a args) bool { return r.handle == a.s },
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndHandle(c, o(a), Handle(a.s)))
			}},
		{
			name: "FindByOrgAndEmail",
			sc:   org,
			vals: vals("e1@x", "E2@X", "e2@x", "missing"),
			pred: func(r trow, a args) bool { return r.email == a.s },
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByOrgAndEmail(c, o(a), a.s)) },
		},
		{
			name: "FindByOrgAndMixedCase",
			sc:   org,
			vals: labelVals,
			pred: label,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndMixedCase(c, o(a), a.s))
			},
		},
		{
			name: "FindByOrgAndInternal",
			sc:   org,
			vals: []args{{b: true}, {b: false}},
			pred: func(r trow, a args) bool { return r.flag == a.b },
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByOrgAndInternal(c, o(a), a.b)) },
		},
		// Internal after a scope filter is no escape: the org still filters.
		{name: "FindByOrgAndNameInternal", sc: org, vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndNameInternal(c, o(a), a.s))
			}},
		{name: "FindByOrgInternal", sc: org,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByOrgInternal(c, o(a))) }},
		{
			name: "FindByInternalInternal",
			vals: []args{{b: true}, {b: false}},
			pred: func(r trow, a args) bool { return r.flag == a.b },
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByInternalInternal(c, a.b)) },
		},
		{name: "FindByNameInternal", vals: nameVals, pred: name,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByNameInternal(c, a.s)) }},
		{name: "FindByIDInternal", vals: labelVals, pred: label, single: true,
			find: func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByIDInternal(c, id(a))) }},
		{name: "FindAllInternal",
			find: func(c context.Context, _ args) ([]string, error) { return lbl(repo.FindAllInternal(c)) }},
		{name: "CountInternal", count: func(c context.Context, _ args) (int64, error) { return repo.CountInternal(c) }},
		{name: "CountByNameInternal", vals: nameVals, pred: name,
			count: func(c context.Context, a args) (int64, error) { return repo.CountByNameInternal(c, a.s) }},
		{name: "ExistsByNameInternal", vals: nameVals, pred: name,
			exists: func(c context.Context, a args) (bool, error) { return repo.ExistsByNameInternal(c, a.s) }},
		// A named type over the column's type converts, on a count and on a delete.
		{name: "CountByOrgIDAndHandle", sc: org, vals: vals("h1", "h2", "missing"),
			pred: func(r trow, a args) bool { return r.handle == a.s },
			count: func(c context.Context, a args) (int64, error) {
				return repo.CountByOrgIDAndHandle(c, OrgID(o(a)), Handle(a.s))
			}},
		{name: "DeleteByOrgIDAndHandle", sc: org, vals: vals("h1", "h2", "missing"),
			pred: func(r trow, a args) bool { return r.handle == a.s },
			del: func(c context.Context, a args) (int64, error) {
				return repo.DeleteByOrgIDAndHandle(c, OrgID(o(a)), Handle(a.s))
			}},
		{
			name:  "CountByOrg",
			sc:    org,
			count: func(c context.Context, a args) (int64, error) { return repo.CountByOrg(c, o(a)) },
		},
		{name: "CountByOrgAndInternal", sc: org, vals: []args{{b: true}, {b: false}},
			pred:  func(r trow, a args) bool { return r.flag == a.b },
			count: func(c context.Context, a args) (int64, error) { return repo.CountByOrgAndInternal(c, o(a), a.b) }},
		{name: "ExistsByOrgAndEmail", sc: org, vals: vals("e1@x", "E2@X", "missing"),
			pred:   func(r trow, a args) bool { return r.email == a.s },
			exists: func(c context.Context, a args) (bool, error) { return repo.ExistsByOrgAndEmail(c, o(a), a.s) }},
		{name: "ExistsByOrgAndID", sc: org, vals: labelVals, pred: label,
			exists: func(c context.Context, a args) (bool, error) { return repo.ExistsByOrgAndID(c, o(a), id(a)) }},
		{name: "FindByOrg", sc: org,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByOrg(c, o(a))) }},
		{name: "DeleteByOrgAndID", sc: org, vals: labelVals, pred: label,
			del: func(c context.Context, a args) (int64, error) { return none(repo.DeleteByOrgAndID(c, o(a), id(a))) }},
		{name: "DeleteByOrgAndExternalID", sc: org, vals: vals("x1", "x2", "x3", "missing"), pred: ext,
			del: func(c context.Context, a args) (int64, error) {
				return affected(repo.DeleteByOrgAndExternalID(c, o(a), a.s))
			}},
		{name: "DeleteByOrgAndName", sc: org, vals: nameVals, pred: name,
			del: func(c context.Context, a args) (int64, error) { return repo.DeleteByOrgAndName(c, o(a), a.s) }},
	}
	runSpecs(t, db, w, specs)
}

// seedLedgers seeds three rows for each of 2 orgs x 2 products x 2 sites x
// workspaces w1, w2: the same seq and memo everywhere. The label is the whole
// key.
func seedLedgers(t *testing.T, db execer) world {
	t.Helper()
	w := world{
		table: "ledger",
		label: "org_id::text||'/'||product_id::text||'/'||site_id::text||'/'||workspace_id||'/'||seq::text",
	}
	for o := range 2 {
		for p := range 2 {
			for s := range 2 {
				for _, ws := range workspaces {
					for seq := int64(1); seq <= 3; seq++ {
						memo := "m1"
						if seq == 3 {
							memo = "m2"
						}
						r := trow{
							label: fmt.Sprintf("%s/%s/%s/%s/%d", orgs[o], products[p], sites[s], ws, seq),
							org:   o, product: p, site: s, ws: ws, seq: seq, memo: memo,
						}
						mustExec(
							t,
							db,
							`INSERT INTO ledger (org_id, product_id, site_id, workspace_id, seq, memo) VALUES ($1,$2,$3,$4,$5,$6)`,
							orgs[o],
							products[p],
							sites[s],
							ws,
							seq,
							memo,
						)
						w.rows = append(w.rows, r)
					}
				}
			}
		}
	}
	return w
}

func ledgerLabel(l model.Ledger) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d", l.OrgID, l.ProductID, l.SiteID, l.WorkspaceID, l.Seq)
}

func TestLedgersIsolation(t *testing.T) {
	db := openTorture(t)
	w := seedLedgers(t, db)
	repo := NewLedgers(db)
	seqVals := []args{{n: 1}, {n: 2}, {n: 9}}
	memoVals := vals("m1", "m2", "missing")
	o := func(a args) uuid.UUID { return orgs[a.org] }
	p := func(a args) uuid.UUID { return products[a.product] }
	s := func(a args) uuid.UUID { return sites[a.site] }
	seq := func(r trow, a args) bool { return r.seq == a.n }
	memo := func(r trow, a args) bool { return r.memo == a.s }
	lbl, optLbl := labs(ledgerLabel), opt(ledgerLabel)
	three := scOrg | scProduct | scSite
	specs := []spec{
		{
			name:   "FindByOrgAndProductAndSiteAndWorkspaceIDAndSeq",
			sc:     three,
			ws:     wsParam,
			vals:   seqVals,
			pred:   seq,
			single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByOrgAndProductAndSiteAndWorkspaceIDAndSeq(c, o(a), p(a), s(a), a.ws, a.n))
			},
		},
		{
			name:   "GetByOrgAndProductAndSiteAndWorkspaceIDAndSeq",
			sc:     three,
			ws:     wsParam,
			vals:   seqVals,
			pred:   seq,
			single: true,
			get:    true,
			find: func(c context.Context, a args) ([]string, error) {
				return one(
					ledgerLabel,
				)(
					repo.GetByOrgAndProductAndSiteAndWorkspaceIDAndSeq(c, o(a), p(a), s(a), a.ws, a.n),
				)
			},
		},
		{name: "FindByOrgAndProductAndSiteAndWorkspace", sc: three, ws: wsCtx,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndSiteAndWorkspace(c, o(a), p(a), s(a)))
			}},
		{name: "FindByOrgAndProductAndSiteAndWorkspaceAndMemo", sc: three, ws: wsCtx, vals: memoVals, pred: memo,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndSiteAndWorkspaceAndMemo(c, o(a), p(a), s(a), a.s))
			}},
		{name: "FindByOrgAndProductAndSiteAndWorkspaceAndSeq", sc: three, ws: wsCtx, vals: seqVals, pred: seq,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndProductAndSiteAndWorkspaceAndSeq(c, o(a), p(a), s(a), a.n))
			}},
		{name: "FindByOrgAndMemoInternal", sc: scOrg, vals: memoVals, pred: memo,
			find: func(c context.Context, a args) ([]string, error) {
				return lbl(repo.FindByOrgAndMemoInternal(c, o(a), a.s))
			}},
		{
			name: "FindAllInternal",
			find: func(c context.Context, _ args) ([]string, error) { return lbl(repo.FindAllInternal(c)) },
		},
		{name: "CountInternal", count: func(c context.Context, _ args) (int64, error) { return repo.CountInternal(c) }},
		{name: "CountByOrgAndProductAndSiteAndWorkspace", sc: three, ws: wsCtx,
			count: func(c context.Context, a args) (int64, error) {
				return repo.CountByOrgAndProductAndSiteAndWorkspace(c, o(a), p(a), s(a))
			}},
		{name: "CountByOrgAndProductAndSiteAndWorkspaceIDAndSeq", sc: three, ws: wsParam, vals: seqVals, pred: seq,
			count: func(c context.Context, a args) (int64, error) {
				return repo.CountByOrgAndProductAndSiteAndWorkspaceIDAndSeq(c, o(a), p(a), s(a), a.ws, a.n)
			}},
		{name: "ExistsByOrgAndProductAndSiteAndWorkspaceIDAndSeq", sc: three, ws: wsParam, vals: seqVals, pred: seq,
			exists: func(c context.Context, a args) (bool, error) {
				return repo.ExistsByOrgAndProductAndSiteAndWorkspaceIDAndSeq(c, o(a), p(a), s(a), a.ws, a.n)
			}},
		{name: "DeleteByOrgAndProductAndSiteAndWorkspaceAndSeq", sc: three, ws: wsCtx, vals: seqVals, pred: seq,
			del: func(c context.Context, a args) (int64, error) {
				return repo.DeleteByOrgAndProductAndSiteAndWorkspaceAndSeq(c, o(a), p(a), s(a), a.n)
			}},
		{name: "DeleteByOrgAndProductAndSiteAndWorkspaceAndMemo", sc: three, ws: wsCtx, vals: memoVals, pred: memo,
			del: func(c context.Context, a args) (int64, error) {
				return none(repo.DeleteByOrgAndProductAndSiteAndWorkspaceAndMemo(c, o(a), p(a), s(a), a.s))
			}},
	}
	runSpecs(t, db, w, specs)
}
