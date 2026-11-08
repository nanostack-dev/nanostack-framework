// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package dbconfig

import (
	"fmt"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

// Postgres test database connection parameters.
// Nanostack: variables, not constants; the testenv harness points them at the containers.
var (
	PgHost     = "localhost"
	PgPort     = 50901
	PgUser     = "jet"
	PgPassword = "jet"
	PgDBName   = "jetdb"
)

// PostgresConnectString is PostgreSQL test database connection string
var PostgresConnectString = pgConnectionString(PgHost, PgPort, PgUser, PgPassword, PgDBName)

// Cockroach test database connection parameters
var (
	CockroachHost     = "localhost"
	CockroachPort     = 26257
	CockroachUser     = "jet"
	CockroachPassword = "jet"
	CockroachDBName   = "jetdb"
)

// CockroachConnectString is Cockroach test database connection string
var CockroachConnectString = pgConnectionString(CockroachHost, CockroachPort, CockroachUser, CockroachPassword, CockroachDBName)

func pgConnectionString(host string, port int, user, password, dbName string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbName)
}

// MySQL test database connection parameters
var (
	MySqLHost     = "127.0.0.1"
	MySQLPort     = 50902
	MySQLUser     = "jet"
	MySQLPassword = "jet"

	MariaDBHost     = "127.0.0.1"
	MariaDBPort     = 50903
	MariaDBUser     = "jet"
	MariaDBPassword = "jet"
)

// MySQLConnectionString is MySQL connection string for test database
func MySQLConnectionString(isMariaDB bool, dbName string) string {
	if isMariaDB {
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", MariaDBUser, MariaDBPassword, MariaDBHost, MariaDBPort, dbName)
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", MySQLUser, MySQLPassword, MySqLHost, MySQLPort, dbName)
}

// sqllite
var (
	SakilaDBPath     = repo.GetTestDataFilePath("/init/sqlite/sakila.db")
	ChinookDBPath    = repo.GetTestDataFilePath("/init/sqlite/chinook.db")
	TestSampleDBPath = repo.GetTestDataFilePath("/init/sqlite/test_sample.db")
)
