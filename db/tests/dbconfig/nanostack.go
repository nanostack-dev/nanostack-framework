package dbconfig

import (
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv/portproxy"
)

// Nanostack: the testenv harness starts the databases on random host ports, then
// calls these setters before the suites run. Users, passwords and database names
// stay the upstream ones because the suites hard-code some of them.

// PostgresProxy owns PgPort for the whole test process. The postgres suite copies
// PgHost and PgPort into a package variable before TestMain runs, and one process runs
// against several PostgreSQL servers, so the port must exist at package init and stay
// fixed; the harness points the proxy at the server of each run.
var PostgresProxy *portproxy.Proxy

func init() {
	proxy, err := portproxy.Listen()
	if err != nil {
		panic(fmt.Sprintf("dbconfig: reserve the PostgreSQL port: %v", err))
	}
	PostgresProxy = proxy
	PgPort = proxy.Port()
	PostgresConnectString = pgConnectionString(PgHost, PgPort, PgUser, PgPassword, PgDBName)
}

// UsePostgres routes PgHost:PgPort to the PostgreSQL server at host:port.
func UsePostgres(host string, port int) {
	PostgresProxy.SetTarget(fmt.Sprintf("%s:%d", host, port))
}

// UseCockroach points the Cockroach* parameters and CockroachConnectString at a CockroachDB server.
func UseCockroach(host string, port int) {
	CockroachHost, CockroachPort = host, port
	CockroachConnectString = pgConnectionString(
		CockroachHost,
		CockroachPort,
		CockroachUser,
		CockroachPassword,
		CockroachDBName,
	)
}

// UseMySQL points the MySQL* parameters at a MySQL server.
func UseMySQL(host string, port int) {
	MySqLHost, MySQLPort = host, port
}

// UseMariaDB points the MariaDB* parameters at a MariaDB server.
func UseMariaDB(host string, port int) {
	MariaDBHost, MariaDBPort = host, port
}

// UseSQLite points the SQLite database paths at copies in dir, so the suites
// never write to the shared test data cache.
func UseSQLite(sakila, chinook, testSample string) {
	SakilaDBPath, ChinookDBPath, TestSampleDBPath = sakila, chinook, testSample
}
