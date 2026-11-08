package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// catalogIndex is one unique index as pg_index describes it.
type catalogIndex struct {
	table     string
	name      string
	columns   []string // key columns only; "" for an expression key
	predicate string
	valid     bool
	immediate bool // false for a DEFERRABLE constraint
	// collationMismatch: a key column has a nondeterministic collation, such
	// as a case-insensitive one, and the index uses another. The index is
	// unique under its own collation, but col = $1 compares under the
	// column's, which can match rows that differ only in case.
	collationMismatch bool
}

// uniqueIndexQuery reads every unique index of the base tables of schema $1,
// primary key first, then by name. indnkeyatts leaves INCLUDE columns out; an
// expression key has attnum 0 and no pg_attribute row, so its name is "".
const uniqueIndexQuery = `
SELECT c.relname, i.relname,
       to_json(ARRAY(SELECT COALESCE(a.attname, '')
                     FROM unnest((ix.indkey::int2[])[0:ix.indnkeyatts - 1]) WITH ORDINALITY AS k(attnum, ord)
                     LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
                     ORDER BY k.ord))::text,
       COALESCE(pg_catalog.pg_get_expr(ix.indpred, ix.indrelid), ''),
       ix.indisvalid, ix.indimmediate,
       EXISTS (SELECT 1
               FROM unnest((ix.indkey::int2[])[0:ix.indnkeyatts - 1],
                           (ix.indcollation::oid[])[0:ix.indnkeyatts - 1]) AS k(attnum, coll)
               JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
               JOIN pg_catalog.pg_collation ac ON ac.oid = a.attcollation
               WHERE k.coll <> a.attcollation AND NOT ac.collisdeterministic)
FROM pg_catalog.pg_index ix
JOIN pg_catalog.pg_class c ON c.oid = ix.indrelid
JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND ix.indisunique
ORDER BY c.relname, ix.indisprimary DESC, i.relname`

// addUniqueIndexes sets the UniqueIndexes of each table, with the reason
// each index is not a key.
func addUniqueIndexes(db *sql.DB, schemaName string, tables []metadata.Table) error {
	indexes, err := readUniqueIndexes(context.Background(), db, schemaName)
	if err != nil {
		return fmt.Errorf("failed to query unique indexes of schema '%s': %w", schemaName, err)
	}
	byTable := map[string][]metadata.UniqueIndex{}
	for _, ix := range indexes {
		byTable[ix.table] = append(byTable[ix.table], metadata.UniqueIndex{
			Name:      ix.name,
			Columns:   ix.columns,
			Predicate: ix.predicate,
			NotKey:    notKey(ix),
		})
	}
	for i := range tables {
		tables[i].UniqueIndexes = byTable[tables[i].Name]
	}
	return nil
}

func readUniqueIndexes(ctx context.Context, db *sql.DB, schemaName string) ([]catalogIndex, error) {
	rows, err := db.QueryContext(ctx, uniqueIndexQuery, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []catalogIndex
	for rows.Next() {
		var ix catalogIndex
		var columns string
		if err = rows.Scan(&ix.table, &ix.name, &columns, &ix.predicate, &ix.valid, &ix.immediate,
			&ix.collationMismatch); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(columns), &ix.columns); err != nil {
			return nil, fmt.Errorf("key columns of %s: %w", ix.name, err)
		}
		out = append(out, ix)
	}
	return out, rows.Err()
}

// notNullPredicate matches one operand of an AND, as pg_get_expr prints it:
// "(col IS NOT NULL)" or a part of "((a IS NOT NULL) AND (b IS NOT NULL))".
var notNullPredicate = regexp.MustCompile(`^\(*([a-z_][a-z0-9_$]*) IS NOT NULL\)*$`)

// notKey says why equality on every key column of ix, with values that are
// not NULL, can match more than one row; "" when it matches at most one. The
// predicates of a derived method are col = $n with a non-NULL value, so an
// "IS NOT NULL" on a key column adds nothing, and any other predicate makes
// a row that fails it a second match.
func notKey(ix catalogIndex) string {
	switch {
	case !ix.valid:
		return "not valid (CREATE INDEX CONCURRENTLY did not finish)"
	case !ix.immediate:
		return "DEFERRABLE: duplicates may exist until commit"
	case slices.Contains(ix.columns, ""):
		return "expression key"
	case ix.collationMismatch:
		return "a key column compares under a nondeterministic collation the index does not use"
	case ix.predicate == "":
		return ""
	}
	for part := range strings.SplitSeq(ix.predicate, " AND ") {
		m := notNullPredicate.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil || !slices.Contains(ix.columns, m[1]) {
			return "partial index WHERE " + ix.predicate
		}
	}
	return ""
}
