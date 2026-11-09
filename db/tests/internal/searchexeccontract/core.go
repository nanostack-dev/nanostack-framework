// This file is Nanostack-only (not in go-jet): the db/searchexec core (R6) on every dialect.
// See db/UPSTREAM.md.

package searchexeccontract

import (
	"cmp"
	"context"
	mathrand "math/rand/v2"
	"slices"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/searchexec"
)

// seRow is the row of the searches. Its fields read the aliases of seFixture.columns, so the
// scan does not depend on the random table names.
type seRow struct {
	ID    int64   `alias:"se_row.id"`
	Org   string  `alias:"se_row.org"`
	Team  string  `alias:"se_row.team_id"`
	Owner *string `alias:"se_row.owner"`
	Space string  `alias:"se_row.workspace"`
	Rank  int64   `alias:"se_row.rank"`
	Score *int64  `alias:"se_row.score"`
	Gone  bool    `alias:"se_row.deleted"`
	// Weight is filled only by the hand-written computed key of the hand-written sort.
	Weight int64 `alias:"se_row.weight"`
}

type seSort string

const (
	seRank    seSort = "rank"
	seScore   seSort = "score"
	seScoreNF seSort = "score_nulls_first"
	seScoreNL seSort = "score_nulls_last"
)

// seScope is the scope struct of the item search, as nano gen repo writes it.
type seScope struct {
	_     struct{} // keyed fields only
	Org   string   // items.org and teams.org
	Team  string   // items.team_id
	Owner string   // items.owner, or NULL
}

type seItemSearch struct {
	searchexec.Query[seRow, seItemSearch, seSort, seScope]
}

// seOrgSearch has one scope value, the organization.
type seOrgSearch struct {
	searchexec.Query[seRow, seOrgSearch, seSort, string]
}

type seWorkspaceKey struct{}

// seWorkspace is the context scope: the workspace of the context, TRUE when the context has
// none, and nil when the context asks for it (a broken scope function).
func seWorkspace(ctx context.Context, column ColumnString) BoolExpression {
	switch v, _ := ctx.Value(seWorkspaceKey{}).(string); v {
	case "":
		return searchexec.BoolLit(true)
	case "nil":
		return nil
	default:
		return column.EQ(searchexec.StringLit(v))
	}
}

type seFixture struct {
	rows []seRow

	id, rank, score                            ColumnInteger
	org, team, owner, workspace, teamID, teamO ColumnString
	deleted                                    ColumnBool

	items       keyset.Source
	items2Teams keyset.Source
	columns     ProjectionList
	keys        map[seSort]keyset.Key[seRow]
	tiebreak    []keyset.Key[seRow]

	itemSpec *searchexec.Spec[seRow, seSort, seScope]
	orgSpec  *searchexec.Spec[seRow, seSort, string]
}

var seTeams = map[string]string{"t1": "org-a", "t2": "org-a", "t3": "org-b"}

// newSeFixture creates the two tables and fills them. The data has few distinct sort
// values, so ties dominate, and NULL scores and owners. Some rows name a team of another
// organization, so only the scope on teams.org drops them. The rank column is named grade:
// RANK is a reserved word in MySQL.
func (s suite) newSeFixture(t *testing.T) *seFixture {
	t.Helper()

	teamsName, itemsName := s.name("nanostack_se_teams"), s.name("nanostack_se_items")
	s.create(t, map[string]string{
		teamsName: "id " + s.text() + " PRIMARY KEY, org " + s.text() + " NOT NULL",
		itemsName: `id BIGINT PRIMARY KEY,
			org ` + s.text() + ` NOT NULL,
			team_id ` + s.text() + ` NOT NULL REFERENCES ` + s.qualified(teamsName) + ` (id),
			owner ` + s.text() + `,
			workspace ` + s.text() + ` NOT NULL,
			grade BIGINT NOT NULL,
			score BIGINT,
			deleted BOOLEAN NOT NULL`,
	}, teamsName, itemsName)
	for id, org := range seTeams {
		s.exec(t, "INSERT INTO "+s.qualified(teamsName)+" (id, org) VALUES (?, ?)", id, org)
	}

	f := &seFixture{}
	rng := mathrand.New(mathrand.NewPCG(6, 9)) //nolint:gosec // a fixed seed, so every dialect gets the same rows
	used := map[int64]bool{}
	for range 160 {
		row := seRow{Org: "org-a", Team: "t1", Space: "w1", Rank: rng.Int64N(3)}
		// Random ids, so the tiebreak order is unrelated to the insertion order.
		for row.ID == 0 || used[row.ID] {
			row.ID = rng.Int64N(1_000_000) + 1
		}
		used[row.ID] = true
		switch rng.IntN(10) {
		case 0:
			row.Org, row.Team = "org-b", "t3"
		case 1:
			row.Team = "t2"
		case 2:
			row.Team = "t3" // a team of org-b: teams.org = org-b
		}
		if owner := []string{"u1", "u2", ""}[rng.IntN(3)]; owner != "" {
			row.Owner = &owner
		}
		if rng.IntN(2) == 0 {
			row.Space = "w2"
		}
		if rng.IntN(3) > 0 {
			score := rng.Int64N(3)
			row.Score = &score
		}
		row.Gone = rng.IntN(10) == 0
		s.exec(t, "INSERT INTO "+s.qualified(itemsName)+
			" (id, org, team_id, owner, workspace, grade, score, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			row.ID, row.Org, row.Team, row.Owner, row.Space, row.Rank, row.Score, row.Gone)
		f.rows = append(f.rows, row)
	}

	f.id, f.rank, f.score = IntegerColumn("id"), IntegerColumn("grade"), IntegerColumn("score")
	f.org, f.team, f.owner = StringColumn("org"), StringColumn("team_id"), StringColumn("owner")
	f.workspace, f.deleted = StringColumn("workspace"), BoolColumn("deleted")
	f.items = s.table(itemsName, f.id, f.org, f.team, f.owner, f.workspace, f.rank, f.score, f.deleted)
	f.teamID, f.teamO = StringColumn("id"), StringColumn("org")
	teams := s.table(teamsName, f.teamID, f.teamO)
	f.items2Teams = s.cfg.InnerJoin(f.items, teams, f.teamID.EQ(f.team))
	f.columns = ProjectionList{
		f.id.AS("se_row.id"), f.org.AS("se_row.org"), f.team.AS("se_row.team_id"), f.owner.AS("se_row.owner"),
		f.workspace.AS("se_row.workspace"), f.rank.AS("se_row.rank"), f.score.AS("se_row.score"),
		f.deleted.AS("se_row.deleted"),
	}
	score := func(r seRow) *int64 { return r.Score }
	f.keys = map[seSort]keyset.Key[seRow]{
		seRank:    keyset.IntKey(f.rank, func(r seRow) int64 { return r.Rank }),
		seScore:   keyset.NullableIntKey(f.score, score),
		seScoreNF: keyset.NullableIntKey(f.score, score).NullsFirst(),
		seScoreNL: keyset.NullableIntKey(f.score, score).NullsLast(),
	}
	f.tiebreak = []keyset.Key[seRow]{keyset.IntKey(f.id, func(r seRow) int64 { return r.ID })}
	f.itemSpec = searchexec.DefineSpec(f.itemDef())
	f.orgSpec = searchexec.DefineSpec(searchexec.SpecDef[seRow, seSort, string]{
		From:    func(string) keyset.Source { return f.items },
		Columns: f.columns,
		Scope: []searchexec.ScopeCol[string]{
			searchexec.ScopeEq(searchexec.Value[string], f.org),
			searchexec.ScopeNull[string](f.owner),
		},
		Keys:     f.keys,
		Tiebreak: f.tiebreak,
		Limit:    50,
	})
	return f
}

// itemDef is the item search: one field scopes items.org and teams.org, the team is scoped
// on items, the owner may be NULL, the workspace comes from the context, and deleted rows
// never show.
func (f *seFixture) itemDef() searchexec.SpecDef[seRow, seSort, seScope] {
	return searchexec.SpecDef[seRow, seSort, seScope]{
		From:    func(seScope) keyset.Source { return f.items2Teams },
		Columns: f.columns,
		Scope: []searchexec.ScopeCol[seScope]{
			searchexec.ScopeEq(func(s seScope) string { return s.Org }, f.org, f.teamO),
			searchexec.ScopeEq(func(s seScope) string { return s.Team }, f.team),
			searchexec.ScopeOrNull(func(s seScope) string { return s.Owner }, f.owner),
		},
		Context:    []searchexec.ContextScope{searchexec.ScopeFunc(seWorkspace, f.workspace)},
		Conditions: []BoolExpression{f.deleted.IS_FALSE()},
		Keys:       f.keys,
		Tiebreak:   f.tiebreak,
		Default:    searchexec.Desc(seRank),
		Limit:      20,
	}
}

func (f *seFixture) search(s seScope) seItemSearch {
	return searchexec.New[seItemSearch](f.itemSpec, s)
}

var seScopeA = seScope{Org: "org-a", Team: "t1", Owner: "u1"}

// match is the item search's filter in Go: the rows the database must return for scope s
// and workspace w ("" for every workspace).
func (f *seFixture) match(s seScope, w string) []seRow {
	var rows []seRow
	for _, r := range f.rows {
		if r.Org == s.Org && seTeams[r.Team] == s.Org && r.Team == s.Team &&
			(r.Owner == nil || *r.Owner == s.Owner) && (w == "" || r.Space == w) && !r.Gone {
			rows = append(rows, r)
		}
	}
	return rows
}

// seCompare orders two rows as the keyset order of sorts does, in Go: each sort with its NULL
// placement, then the id in the direction of the last sort.
func seCompare(sorts []search.Sort[seSort]) func(a, b seRow) int {
	return func(a, b seRow) int {
		last := search.SortAscending
		for _, s := range sorts {
			last = s.Direction
			desc := s.Direction == search.SortDescending
			var c int
			switch s.Field {
			case seRank:
				c = cmp.Compare(a.Rank, b.Rank)
				if desc {
					c = -c
				}
			case seScore, seScoreNF, seScoreNL:
				nullsLast := !desc
				if s.Field == seScoreNF {
					nullsLast = false
				} else if s.Field == seScoreNL {
					nullsLast = true
				}
				c = compareNullable(a.Score, b.Score, desc, nullsLast)
			}
			if c != 0 {
				return c
			}
		}
		if last == search.SortDescending {
			return cmp.Compare(b.ID, a.ID)
		}
		return cmp.Compare(a.ID, b.ID)
	}
}

func compareNullable(a, b *int64, desc, nullsLast bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		if nullsLast {
			return 1
		}
		return -1
	case b == nil:
		if nullsLast {
			return -1
		}
		return 1
	case desc:
		return cmp.Compare(*b, *a)
	default:
		return cmp.Compare(*a, *b)
	}
}

func seIDs(rows []seRow) []int64 {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func seIdentity(r seRow) seRow { return r }

// skipNullOrder skips a sort on the nullable score on CockroachDB: the keyset pager writes
// the PostgreSQL default NULL placement (last when ascending), and CockroachDB sorts NULLs
// first. ADR 0015 drops CockroachDB.
func (s suite) skipNullOrder(t *testing.T, sorts []search.Sort[seSort]) {
	t.Helper()
	if s.cfg.Flavor == CockroachDB && slices.ContainsFunc(sorts, func(s search.Sort[seSort]) bool {
		return s.Field == seScore
	}) {
		t.Skip("CockroachDB places NULLs first in ascending order")
	}
}
