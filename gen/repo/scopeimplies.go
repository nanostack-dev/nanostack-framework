package genrepo

import (
	"slices"
	"strconv"
)

// ScopeImpliesProblem is one refusal of a Postgres.ScopeImplies map.
type ScopeImpliesProblem struct {
	Msg string // what is wrong
	Fix string // how to fix it
}

// CheckScopeImplies checks a ScopeImplies map against the scope columns and
// the context columns of a project: both sides of an entry are in scopes,
// neither is a context column, and the map has no cycle. nanostack gen reads
// the project file through it, and the -scope-implies flag through the same
// function, so the two guard the map alike. subject names the map in the
// messages: "Postgres.ScopeImplies" or "-scope-implies".
func CheckScopeImplies(subject string, scopes, contextCols []string, implies map[string]string) []ScopeImpliesProblem {
	cols := make([]string, 0, len(implies))
	for col := range implies {
		cols = append(cols, col)
	}
	slices.Sort(cols)
	var out []ScopeImpliesProblem
	for _, col := range cols {
		to := implies[col]
		for _, side := range []string{col, to} {
			switch {
			case !slices.Contains(scopes, side):
				out = append(out, ScopeImpliesProblem{
					Msg: subject + " names " + strconv.Quote(side) + ", which is not in Postgres.Scopes",
					Fix: "add " + strconv.Quote(side) + " to Postgres.Scopes, or remove the entry"})
			case slices.Contains(contextCols, side):
				out = append(out, ScopeImpliesProblem{
					Msg: subject + " names " + strconv.Quote(side) + ", a context column (Postgres.ContextScopes)",
					Fix: "remove the entry: a context scope is named in every derived method, and implies nothing"})
			}
		}
		seen := map[string]bool{}
		for next, ok := to, true; ok && !seen[next]; next, ok = implies[next] {
			if next == col {
				out = append(out, ScopeImpliesProblem{
					Msg: subject + " has a cycle through " + strconv.Quote(col),
					Fix: "break the cycle: a column cannot determine itself"})
				break
			}
			seen[next] = true
		}
	}
	return out
}
