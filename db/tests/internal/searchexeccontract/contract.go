// This file is Nanostack-only (not in go-jet): the cross-dialect suite of db/searchexec
// (design-search R6, R7 and R10). See db/UPSTREAM.md.

// Package searchexeccontract is the suite of the search runtime, written once and run on
// PostgreSQL, CockroachDB, MySQL, MariaDB and SQLite. Each search is a SpecDef written by
// hand as nano gen repo writes it, and runs against tables that the suite creates with new
// random names, so a statement-cache run never meets a cached statement of a dropped table.
//
// The text columns use a binary collation on MySQL and MariaDB, so the answers that Go
// computes hold on every dialect. The checks that differ per dialect name the flavor.
package searchexeccontract

import (
	"context"
	"crypto/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
)

// Flavor is the database a run targets.
type Flavor string

// The flavors of the suite.
const (
	Postgres    Flavor = "postgres"
	CockroachDB Flavor = "cockroachdb"
	MySQL       Flavor = "mysql"
	MariaDB     Flavor = "mariadb"
	SQLite      Flavor = "sqlite"
)

// Config is what a dialect's test package gives the suite.
type Config struct {
	Flavor Flavor
	// DB holds the tables. A MySQL or MariaDB connection reads times as time.Time in UTC
	// (parseTime=true&loc=UTC).
	DB qrm.DB
	// Schema is the schema of the tables, as the dialect's NewTable takes it.
	Schema string
	// NewTable is the dialect package's NewTable.
	NewTable func(schema, name string, columns ...postgres.Column) keyset.Source
	// InnerJoin joins two tables of NewTable with the dialect package's INNER_JOIN.
	InnerJoin func(left, right keyset.Source, on postgres.BoolExpression) keyset.Source
}

// Run runs the suite on one database.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	s := suite{cfg: cfg}
	t.Run("core", s.runCore)
	t.Run("fields", s.runFields)
	t.Run("two tenants", s.runPrecedence)
}

type suite struct{ cfg Config }

func (s suite) mysqlLike() bool { return s.cfg.Flavor == MySQL || s.cfg.Flavor == MariaDB }

func (s suite) postgresLike() bool { return s.cfg.Flavor == Postgres || s.cfg.Flavor == CockroachDB }

// name returns a table name that no other run uses.
func (s suite) name(prefix string) string {
	return prefix + "_" + strings.ToLower(rand.Text())
}

// qualified is the table name as raw SQL writes it.
func (s suite) qualified(name string) string {
	if s.cfg.Schema == "" {
		return name
	}
	return s.cfg.Schema + "." + name
}

// sql replaces each ? of query with $1, $2, ... on PostgreSQL and CockroachDB.
func (s suite) sql(query string) string {
	if !s.postgresLike() {
		return query
	}
	var out strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			out.WriteString("$" + strconv.Itoa(n))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func (s suite) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := s.cfg.DB.ExecContext(t.Context(), s.sql(query), args...)
	require.NoError(t, err, query)
}

// create runs the CREATE TABLE statements and drops the tables in reverse order at the end
// of the test.
func (s suite) create(t *testing.T, tables map[string]string, order ...string) {
	t.Helper()
	for _, name := range order {
		s.exec(t, "CREATE TABLE "+s.qualified(name)+" ("+tables[name]+")")
	}
	t.Cleanup(func() {
		for i := len(order) - 1; i >= 0; i-- {
			_, err := s.cfg.DB.ExecContext(context.Background(), "DROP TABLE "+s.qualified(order[i]))
			require.NoError(t, err)
		}
	})
}

// text is the type of a text column: a binary collation on MySQL and MariaDB, so the
// answers that Go computes hold there too.
func (s suite) text() string {
	switch s.cfg.Flavor {
	case MySQL:
		return "VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"
	case MariaDB:
		return "VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin"
	case Postgres, CockroachDB, SQLite:
	}
	return "TEXT"
}

// cText is the type of a text column whose collation folds ASCII letters only: "C" on
// PostgreSQL. CockroachDB has no "C" collation; the other flavors use the binary text.
func (s suite) cText() string {
	if s.cfg.Flavor == Postgres {
		return `TEXT COLLATE "C"`
	}
	return s.text()
}

// timestamptz is the type of a time column: an instant.
func (s suite) timestamptz() string {
	switch s.cfg.Flavor {
	case MySQL, MariaDB:
		return "DATETIME(6)"
	case SQLite:
		return "DATETIME"
	case Postgres, CockroachDB:
	}
	return "TIMESTAMPTZ"
}

// timestamp is the type of a time column without time zone: a wall clock, in UTC here.
func (s suite) timestamp() string {
	if s.postgresLike() {
		return "TIMESTAMP"
	}
	return s.timestamptz()
}

// uuid is the type of a UUID column.
func (s suite) uuid() string {
	switch s.cfg.Flavor {
	case MySQL, MariaDB:
		return "CHAR(36)"
	case SQLite:
		return "TEXT"
	case Postgres, CockroachDB:
	}
	return "UUID"
}

func (s suite) table(name string, columns ...postgres.Column) keyset.Source {
	return s.cfg.NewTable(s.cfg.Schema, name, columns...)
}
