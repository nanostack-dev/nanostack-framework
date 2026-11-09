package pipeline

import (
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
)

// scopes checks Postgres.Scopes: each entry a column name, none twice.
func (c *projectCheck) scopes(scopes []string) {
	seen := map[string]bool{}
	for _, scope := range scopes {
		switch {
		case !isColumnName(scope):
			c.add(c.at("Postgres.Scopes"), "write each scope as the column name, such as \"product_id\"",
				"Postgres.Scopes entry %q is not a lower-case column name", scope)
		case seen[scope]:
			c.add(c.at("Postgres.Scopes"), "remove the second "+strconv.Quote(scope),
				"Postgres.Scopes lists %q twice", scope)
		}
		seen[scope] = true
	}
}

// contextScopes checks Postgres.ContextScopes: each column a column of
// Scopes, once, with a function written as an import path, a dot and a
// function name. nanostack gen repo checks the function itself where a search
// names it: the project check loads no package.
func (c *projectCheck) contextScopes(scopes []string, ctx []project.ContextScope) {
	const fix = `write the function as "example.com/app/internal/db/workspacescope.Filter"`
	seen := map[string]bool{}
	for _, cs := range ctx {
		switch {
		case !slices.Contains(scopes, cs.Column):
			c.add(c.at("Postgres.ContextScopes"), "add "+strconv.Quote(cs.Column)+" to Postgres.Scopes, or "+
				"remove its context scope", "Postgres.ContextScopes names %q, which is not in Postgres.Scopes",
				cs.Column)
		case seen[cs.Column]:
			c.add(c.at("Postgres.ContextScopes"), "register one function per column",
				"Postgres.ContextScopes names %q twice", cs.Column)
		case !funcPath(cs.Func):
			c.add(c.at("Postgres.ContextScopes"), fix, "Postgres.ContextScopes: the function of %q is %q, not "+
				"an import path, a dot and a function name", cs.Column, cs.Func)
		}
		seen[cs.Column] = true
	}
}

// scopeNames checks Postgres.ScopeNames: each key a column of Scopes, each
// value an exported Go identifier, no name given to two columns.
func (c *projectCheck) scopeNames(scopes []string, names map[string]string) {
	cols := make([]string, 0, len(names))
	for col := range names {
		cols = append(cols, col)
	}
	slices.Sort(cols)
	owner := map[string]string{}
	for _, col := range cols {
		name := names[col]
		switch {
		case !slices.Contains(scopes, col):
			c.add(c.at("Postgres.ScopeNames"), "add "+strconv.Quote(col)+" to Postgres.Scopes, or remove its name",
				"Postgres.ScopeNames names %q, which is not in Postgres.Scopes", col)
		case !token.IsExported(name) || !token.IsIdentifier(name):
			c.add(c.at("Postgres.ScopeNames"), "write an exported Go identifier, such as \"Org\"",
				"Postgres.ScopeNames: the name of %q is %q, not an exported Go identifier", col, name)
		case owner[name] != "":
			c.add(c.at("Postgres.ScopeNames"), "give each column its own name",
				"Postgres.ScopeNames: %q and %q share the name %q", owner[name], col, name)
		default:
			owner[name] = col
		}
	}
}

// funcPath reports whether s is an import path, a dot and an identifier:
// example.com/app/scope.Filter.
func funcPath(s string) bool {
	dot := strings.LastIndex(s, ".")
	if dot <= 0 || dot < strings.LastIndex(s, "/") {
		return false
	}
	return token.IsIdentifier(s[dot+1:])
}

// isColumnName reports whether s is a column name as nanostack gen db reads
// it: lower-case letters, digits and underscores, not starting with a digit.
func isColumnName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
