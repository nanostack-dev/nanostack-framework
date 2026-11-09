//go:build integration

package torture

import (
	"context"
	"fmt"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture/db/torture/public/model"
)

// seedWorkspaced seeds rows of note or draft, which have only a context
// scope: two rows each in w1 and w2, and for draft two with no workspace.
func seedWorkspaced(t *testing.T, db execer, table string, withNull bool) world {
	t.Helper()
	w := world{table: table, label: "id"}
	wss := slicesClone(workspaces)
	if withNull {
		wss = append(wss, "")
	}
	for _, ws := range wss {
		for k := 1; k <= 2; k++ {
			r := trow{
				label:  fmt.Sprintf("%s-%s-%d", table, cmpOr(ws, "null"), k),
				ws:     ws,
				wsNull: ws == "",
				name:   "same",
			}
			if k == 2 {
				r.name = "other"
			}
			var wsArg any
			if ws != "" {
				wsArg = ws
			}
			mustExec(t, db, `INSERT INTO `+table+` (id, workspace_id, title) VALUES ($1,$2,$3)`, r.label, wsArg, r.name)
			w.rows = append(w.rows, r)
		}
	}
	return w
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

func TestNotesIsolation(t *testing.T) {
	db := openTorture(t)
	w := seedWorkspaced(t, db, "note", false)
	repo := NewNotes(db)
	idVals := vals("note-w1-1", "note-w2-2", "missing")
	titleVals := vals("same", "other", "missing")
	label := func(r trow, a args) bool { return r.label == a.s }
	title := func(r trow, a args) bool { return r.name == a.s }
	noteLabel := func(n model.Note) string { return n.ID }
	lbl, optLbl := labs(noteLabel), opt(noteLabel)
	specs := []spec{
		{
			name:   "FindByWorkspaceAndID",
			ws:     wsCtx,
			vals:   idVals,
			pred:   label,
			single: true,
			find:   func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByWorkspaceAndID(c, a.s)) },
		},
		{name: "GetByWorkspaceAndID", ws: wsCtx, vals: idVals, pred: label, single: true, get: true,
			find: func(c context.Context, a args) ([]string, error) {
				return one(noteLabel)(repo.GetByWorkspaceAndID(c, a.s))
			}},
		{
			name:   "FindByWorkspaceIDAndID",
			ws:     wsParam,
			vals:   idVals,
			pred:   label,
			single: true,
			find: func(c context.Context, a args) ([]string, error) {
				return optLbl(repo.FindByWorkspaceIDAndID(c, a.ws, a.s))
			},
		},
		{
			name: "FindByWorkspaceAndTitle",
			ws:   wsCtx,
			vals: titleVals,
			pred: title,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByWorkspaceAndTitle(c, a.s)) },
		},
		{name: "FindByWorkspace", ws: wsCtx,
			find: func(c context.Context, _ args) ([]string, error) { return lbl(repo.FindByWorkspace(c)) }},
		{name: "CountByWorkspace", ws: wsCtx,
			count: func(c context.Context, _ args) (int64, error) { return repo.CountByWorkspace(c) }},
		{name: "ExistsByWorkspaceAndTitle", ws: wsCtx, vals: titleVals, pred: title,
			exists: func(c context.Context, a args) (bool, error) { return repo.ExistsByWorkspaceAndTitle(c, a.s) }},
		{
			name: "DeleteByWorkspaceAndID",
			ws:   wsCtx,
			vals: idVals,
			pred: label,
			del:  func(c context.Context, a args) (int64, error) { return affected(repo.DeleteByWorkspaceAndID(c, a.s)) },
		},
		{name: "DeleteByWorkspaceAndTitle", ws: wsCtx, vals: titleVals, pred: title,
			del: func(c context.Context, a args) (int64, error) { return repo.DeleteByWorkspaceAndTitle(c, a.s) }},
		{name: "DeleteByWorkspaceIDAndID", ws: wsParam, vals: idVals, pred: label,
			del: func(c context.Context, a args) (int64, error) {
				return none(repo.DeleteByWorkspaceIDAndID(c, a.ws, a.s))
			}},
		{name: "FindByIDInternal", vals: idVals, pred: label, single: true,
			find: func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByIDInternal(c, a.s)) }},
		{
			name: "FindAllInternal",
			find: func(c context.Context, _ args) ([]string, error) { return lbl(repo.FindAllInternal(c)) },
		},
		{name: "CountInternal", count: func(c context.Context, _ args) (int64, error) { return repo.CountInternal(c) }},
	}
	runSpecs(t, db, w, specs)
}

func TestDraftsIsolation(t *testing.T) {
	db := openTorture(t)
	w := seedWorkspaced(t, db, "draft", true)
	repo := NewDrafts(db)
	idVals := vals("draft-w1-1", "draft-w2-2", "draft-null-1", "missing")
	titleVals := vals("same", "other", "missing")
	label := func(r trow, a args) bool { return r.label == a.s }
	title := func(r trow, a args) bool { return r.name == a.s }
	draftLabel := func(n model.Draft) string { return n.ID }
	lbl, optLbl := labs(draftLabel), opt(draftLabel)
	specs := []spec{
		{
			name:   "FindByWorkspaceAndID",
			ws:     wsCtx,
			vals:   idVals,
			pred:   label,
			single: true,
			find:   func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByWorkspaceAndID(c, a.s)) },
		},
		{
			name: "FindByWorkspaceAndTitle",
			ws:   wsCtx,
			vals: titleVals,
			pred: title,
			find: func(c context.Context, a args) ([]string, error) { return lbl(repo.FindByWorkspaceAndTitle(c, a.s)) },
		},
		{name: "CountByWorkspace", ws: wsCtx,
			count: func(c context.Context, _ args) (int64, error) { return repo.CountByWorkspace(c) }},
		{name: "DeleteByWorkspaceAndTitle", ws: wsCtx, vals: titleVals, pred: title,
			del: func(c context.Context, a args) (int64, error) { return repo.DeleteByWorkspaceAndTitle(c, a.s) }},
		{name: "FindByIDInternal", vals: idVals, pred: label, single: true,
			find: func(c context.Context, a args) ([]string, error) { return optLbl(repo.FindByIDInternal(c, a.s)) }},
	}
	runSpecs(t, db, w, specs)
}
