package keyset

import (
	"fmt"
	"strings"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
)

type orderedKey[T any] struct {
	key       Key[T]
	direction search.SortDirection
}

// Order is a total keyset ordering over rows of T: the requested sort keys,
// then a unique tiebreak. The tiebreak is unique, so every row has exactly
// one position. A cursor (the last row's values) splits the result into the
// rows before it and the rows after it, with no skip and no repeat, however
// rows are inserted or deleted between pages.
//
// Build one with OrderOf and give it to PageBuilder.Keyset. A repository whose
// query does not fit PageBuilder uses OrderBy, Seek and Trim directly.
type Order[T any] struct {
	keys      []orderedKey[T]
	signature string
}

// OrderOf builds the keyset ordering of a search: the requested sorts, then
// the tiebreak keys.
//
// A requested sort absent from keys is search.ErrUnknownSort, naming it:
// ordering by another column than the client asked answers another question.
// A search resolves its documented default to a key before it calls OrderOf.
//
// The tiebreak keys together must be unique and NOT NULL within the rows the
// search can return, normally the primary key. A table with a composite key
// passes every key column that the search scope does not already fix, for
// example the member id for a search scoped to one organization, or both
// columns otherwise. The tiebreak sorts in the direction of the last requested
// sort (ascending when there is none), so a composite index on (sort, id)
// serves the query in one scan. A column requested twice keeps its first
// position, and a tiebreak column that was requested is not repeated.
func OrderOf[S ~string, T any](
	sorts []search.Sort[S], keys map[S]Key[T], tiebreak Key[T], more ...Key[T],
) (Order[T], error) {
	var unknown []string
	for _, sort := range sorts {
		if _, ok := keys[sort.Field]; !ok {
			unknown = append(unknown, string(sort.Field))
		}
	}
	if len(unknown) > 0 {
		return Order[T]{}, fmt.Errorf("%w: %s", search.ErrUnknownSort, strings.Join(unknown, ", "))
	}
	order := Order[T]{}
	seen := map[string]bool{}
	direction := search.SortAscending
	for _, sort := range sorts {
		key := keys[sort.Field]
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		direction = normalizeDirection(sort.Direction)
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	for _, key := range append([]Key[T]{tiebreak}, more...) {
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	order.signature = signatureOf(order.keys)
	return order, nil
}

// signatureOf names each key by its column, direction and NULL placement. It
// names columns, not SQL, so the same walk gets the same cursor on any
// dialect.
func signatureOf[T any](keys []orderedKey[T]) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = qualifiedName(k.key.column) + ":" + string(k.direction)
		switch k.key.nulls {
		case nullsFirst:
			parts[i] += ":NULLS_FIRST"
		case nullsLast:
			parts[i] += ":NULLS_LAST"
		case nullsDefault:
		}
	}
	return strings.Join(parts, ",")
}

func normalizeDirection(direction search.SortDirection) search.SortDirection {
	if direction == search.SortDescending {
		return search.SortDescending
	}
	return search.SortAscending
}

// qualifiedName identifies a column by the SQL that references it, so two
// keys collide only when they are the same column. A column projected from a
// subquery renders as "alias.table.column" and stays distinct from its base
// table column.
func qualifiedName(column jet.Column) string {
	const marker = " IS NULL"
	rendered := jet.SELECT(jet.Int(1)).WHERE(column.IS_NULL()).DebugSql()
	if _, after, ok := strings.Cut(rendered, "WHERE "); ok {
		if before, _, found := strings.Cut(after, marker); found {
			return strings.ReplaceAll(strings.Trim(before, "()"), `"`, "")
		}
	}
	if table := column.TableName(); table != "" {
		return table + "." + column.Name()
	}
	return column.Name()
}

// Signature identifies the ordering, for example
// "widgets.created_at:DESC,widgets.id:DESC". Cursors embed it.
func (o Order[T]) Signature() string {
	return o.signature
}

// OrderBy returns the ORDER BY clauses, tiebreak included. The PostgreSQL
// defaults apply to NULLs, unless a key asks for NullsFirst or NullsLast:
// last when ascending, first when descending. Seek relies on exactly that
// placement.
func (o Order[T]) OrderBy() []jet.OrderByClause {
	clauses := make([]jet.OrderByClause, len(o.keys))
	for i, k := range o.keys {
		clause := k.key.column.ASC()
		if k.direction == search.SortDescending {
			clause = k.key.column.DESC()
		}
		switch k.key.nulls {
		case nullsFirst:
			clause = clause.NULLS_FIRST()
		case nullsLast:
			clause = clause.NULLS_LAST()
		case nullsDefault:
		}
		clauses[i] = clause
	}
	return clauses
}
