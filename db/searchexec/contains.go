package searchexec

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// escapeLike escapes the LIKE wildcards and the escape character of a term, in one pass.
// The escape character is '!', not '\': '!' has no special meaning in a string literal of
// any dialect, so the clause ESCAPE '!' is the same SQL text on PostgreSQL, MySQL (in any
// sql_mode) and SQLite. '\' is an ordinary character of the term.
var escapeLike = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// Contains keeps the rows where at least one of exprs contains term, without case. It is
// the substring rule of every search (see the package documentation for the SQL of each
// dialect and the collation rules).
//
// Contains trims the white space around term. A blank term returns the literal TRUE, so it
// filters nothing; Contains never returns nil, and its result always composes with AND and
// OR. Each expression gets its own match and bound pattern. The matches of several
// expressions are combined with OR in parentheses, so scope.AND(Contains(term, a, b)) keeps
// the scope on every row.
//
// A term that is not valid UTF-8, or that holds a NUL character, returns the literal
// FALSE. A UTF-8 text column cannot hold such a value, and PostgreSQL text cannot hold NUL,
// so no row can match. Without this rule PostgreSQL refuses the statement (SQLSTATE 22021)
// and MySQL can match every row, so the answer is the same on every dialect. The search
// field types pass such a term to Contains, so it matches no row there too.
//
// Contains does not limit the length of term. The search field types refuse a term longer
// than search.MaxTermLength before they call it.
//
// Contains panics if exprs is empty or holds a nil expression: the call is wrong whatever
// the term.
func Contains(term string, exprs ...jet.StringExpression) jet.BoolExpression {
	if len(exprs) == 0 {
		panic("searchexec: Contains needs at least one expression")
	}

	for i, expr := range exprs {
		if expr == nil {
			panic(fmt.Sprintf("searchexec: Contains expression %d is nil", i))
		}
	}

	trimmed := strings.TrimSpace(term)
	if trimmed == "" {
		return sqlTrue()
	}

	if !utf8.ValidString(trimmed) || strings.IndexByte(trimmed, 0) >= 0 {
		return sqlFalse()
	}

	pattern := "%" + escapeLike.Replace(trimmed) + "%"
	if len(exprs) == 1 {
		return likeFold(exprs[0], pattern)
	}

	matches := make([]jet.BoolExpression, len(exprs))
	for i, expr := range exprs {
		matches[i] = likeFold(expr, pattern)
	}

	return jet.OR(matches...)
}

// likeFold matches expr against an escaped pattern without case. Each call binds the
// pattern once. The PostgreSQL dialect also serves CockroachDB; every other dialect,
// also one that is not known, gets the LOWER form.
func likeFold(expr jet.StringExpression, pattern string) jet.BoolExpression {
	return jet.BoolDialectSwitch(func(d jet.Dialect) jet.BoolExpression {
		if d == postgres.Dialect {
			return jet.BoolExp(jet.CustomExpression(
				expr, jet.Token("ILIKE"), jet.String(pattern), jet.Token("ESCAPE '!'"),
			))
		}

		return jet.BoolExp(jet.CustomExpression(
			jet.LOWER(expr), jet.Token("LIKE"), jet.LOWER(jet.String(pattern)), jet.Token("ESCAPE '!'"),
		))
	})
}

// sqlTrue is the literal TRUE, with no bound argument.
func sqlTrue() jet.BoolExpression {
	return jet.BoolExp(jet.AtomicCustomExpression(jet.Token("TRUE")))
}

// sqlFalse is the literal FALSE, with no bound argument.
func sqlFalse() jet.BoolExpression {
	return jet.BoolExp(jet.AtomicCustomExpression(jet.Token("FALSE")))
}

// ContainsAny keeps the rows where at least one of exprs contains at least one of terms:
// Contains for each term, joined with OR in parentheses. No terms return the literal FALSE,
// so a Go caller that builds an empty list gets no row; the API mapper reads an empty list
// as no filter and does not call it. A blank term among terms gives TRUE, as in Contains,
// so it matches every row.
//
// ContainsAny does not limit the number or the length of terms. The search field types
// refuse more than search.MaxInValues terms and a term longer than search.MaxTermLength
// before they call it.
//
// ContainsAny panics if exprs is empty or holds a nil expression, as Contains does.
func ContainsAny(terms []string, exprs ...jet.StringExpression) jet.BoolExpression {
	if len(terms) == 0 {
		Contains("", exprs...) // the checks of exprs
		return sqlFalse()
	}
	if len(terms) == 1 {
		return Contains(terms[0], exprs...)
	}
	matches := make([]jet.BoolExpression, len(terms))
	for i, term := range terms {
		matches[i] = Contains(term, exprs...)
	}
	return jet.OR(matches...)
}
