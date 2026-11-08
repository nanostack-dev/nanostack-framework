// Package testenv is the Nanostack integration-test harness. It replaces go-jet's
// docker-compose file, Makefile, git submodule and init program: one call from a
// suite's TestMain fetches the pinned test data, starts the databases with
// testcontainers, loads them, regenerates the models and runs the suite once per
// selected database.
package testenv

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// Source is one database the suites run against.
type Source string

// Supported sources. PostgreSQL runs on the oldest supported and the current major version.
const (
	Postgres14 Source = "postgres14"
	Postgres18 Source = "postgres18"
	Cockroach  Source = "cockroach"
	MySQL      Source = "mysql"
	MariaDB    Source = "mariadb"
	SQLite     Source = "sqlite"
)

// AllSources lists every source in run order.
var AllSources = []Source{Postgres14, Postgres18, Cockroach, MySQL, MariaDB, SQLite}

// images pins one image per container source. Bump them here; CI and local runs share them.
// SQLite runs in-process and has none.
//
//exhaustive:ignore
var images = map[Source]string{
	Postgres14: "postgres:14.24",
	Postgres18: "postgres:18.6",
	Cockroach:  "cockroachdb/cockroach:v26.3.5",
	MySQL:      "mysql:8.4.13",
	MariaDB:    "mariadb:11.4.13",
}

// Image returns the container image of s, or "" for SQLite.
func Image(s Source) string { return images[s] }

// IsPostgres reports whether s is a PostgreSQL server (any version).
func (s Source) IsPostgres() bool { return s == Postgres14 || s == Postgres18 }

// needsContainer reports whether s runs in a container.
func (s Source) needsContainer() bool { return s != SQLite }

// DialectsEnv selects the sources to run, as a comma-separated list.
const DialectsEnv = "NANODB_DIALECTS"

// aliases expand to several sources.
var aliases = map[string][]Source{
	"all":      AllSources,
	"postgres": {Postgres14, Postgres18},
}

// Selected returns the sources chosen by NANODB_DIALECTS, in run order.
// An empty or unset variable selects every source; a value that names no source
// (only commas and spaces) is an error.
func Selected() ([]Source, error) {
	return parseSources(os.Getenv(DialectsEnv))
}

func parseSources(value string) ([]Source, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return slices.Clone(AllSources), nil
	}

	chosen := make(map[Source]bool)
	for token := range strings.SplitSeq(value, ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if expanded, ok := aliases[token]; ok {
			for _, s := range expanded {
				chosen[s] = true
			}
			continue
		}
		if !slices.Contains(AllSources, Source(token)) {
			return nil, fmt.Errorf(
				"%s: unknown dialect %q (want all, postgres, postgres14, postgres18, cockroach, mysql, mariadb, sqlite)",
				DialectsEnv,
				token,
			)
		}
		chosen[Source(token)] = true
	}

	if len(chosen) == 0 {
		// Only separators: a misconfigured variable must not turn every package green
		// with nothing run.
		return nil, fmt.Errorf(
			"%s=%q names no dialect (want all, postgres, postgres14, postgres18, cockroach, mysql, mariadb, sqlite)",
			DialectsEnv,
			value,
		)
	}

	var out []Source
	for _, s := range AllSources {
		if chosen[s] {
			out = append(out, s)
		}
	}
	return out, nil
}

// StmtCacheEnv set to "true" runs the suites with statement caching enabled, several
// times in a row (upstream's JET_TESTS_WITH_STMT_CACHE).
const StmtCacheEnv = "NANODB_STMT_CACHE"
