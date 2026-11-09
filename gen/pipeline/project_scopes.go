package pipeline

import "strconv"

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

// isColumnName reports whether s is a column name as nano gen db reads
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
