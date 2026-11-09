package postgres

import "testing"

// The predicates are pg_get_expr output, copied from a TimescaleDB catalogue
// with anchor's migrations and eight extra indexes on product_users.
func TestNotKey(t *testing.T) {
	t.Parallel()
	key := func(cols []string, pred string) catalogIndex {
		return catalogIndex{columns: cols, predicate: pred, valid: true, immediate: true}
	}
	for _, tc := range []struct {
		name string
		ix   catalogIndex
		want string
	}{
		{"plain unique", key([]string{"product_id", "email"}, ""), ""},
		{"INCLUDE column dropped from the key", key([]string{"product_id", "email"}, ""), ""},
		{"case-insensitive collation on the index only", key([]string{"product_id", "email"}, ""), ""},
		{"key column IS NOT NULL", key([]string{"product_id", "external_id"}, "(external_id IS NOT NULL)"), ""},
		{
			"two key columns IS NOT NULL",
			key([]string{"product_id", "created_at", "updated_at"},
				"((created_at IS NOT NULL) AND (updated_at IS NOT NULL))"),
			"",
		},
		{
			"other predicate",
			key([]string{"product_id", "name"}, "((status)::text = 'ACTIVE'::text)"),
			"partial index WHERE ((status)::text = 'ACTIVE'::text)",
		},
		{
			"IS NOT NULL on a column outside the key",
			key([]string{"product_id", "status"}, "(external_id IS NOT NULL)"),
			"partial index WHERE (external_id IS NOT NULL)",
		},
		{
			"OR predicate",
			key([]string{"product_id", "updated_at"}, "((updated_at IS NOT NULL) OR (name IS NULL))"),
			"partial index WHERE ((updated_at IS NOT NULL) OR (name IS NULL))",
		},
		{
			"IS NOT NULL and another predicate",
			key([]string{"product_id", "external_id"}, "((external_id IS NOT NULL) AND (deleted_at IS NULL))"),
			"partial index WHERE ((external_id IS NOT NULL) AND (deleted_at IS NULL))",
		},
		{"IS NULL predicate", key([]string{"product_id", ""}, "(organization_id IS NULL)"), "expression key"},
		{"expression key", key([]string{"product_id", ""}, ""), "expression key"},
		{
			"expression key with a key-column predicate",
			key([]string{"organization_id", ""}, "(organization_id IS NOT NULL)"),
			"expression key",
		},
		{
			"not valid",
			catalogIndex{columns: []string{"id"}, immediate: true},
			"not valid (CREATE INDEX CONCURRENTLY did not finish)",
		},
		{"deferrable", catalogIndex{columns: []string{"id"}, valid: true}, "DEFERRABLE: duplicates may exist until commit"},
		{
			"collation mismatch",
			catalogIndex{columns: []string{"email"}, valid: true, immediate: true, collationMismatch: true},
			"a key column compares under a nondeterministic collation the index does not use",
		},
	} {
		if got := notKey(tc.ix); got != tc.want {
			t.Errorf("%s: notKey = %q, want %q", tc.name, got, tc.want)
		}
	}
}
