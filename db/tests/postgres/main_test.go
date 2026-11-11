// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/stmtcache"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ctx = context.Background()

var db *stmtcache.DB
var testRoot string

var source string
var withStatementCaching bool

// currentEnv is the testenv run in progress (Nanostack harness).
var currentEnv *testenv.Env

const CockroachDB = "COCKROACH_DB"
const Postgres = "POSTGRESQL_DB"

func init() {
	withStatementCaching = os.Getenv(testenv.StmtCacheEnv) == "true"
}

func sourceIsCockroachDB() bool {
	return source == CockroachDB
}

func sourceIsPostgres() bool {
	return source == Postgres
}

func skipForCockroachDB(t *testing.T) {
	if sourceIsCockroachDB() {
		t.SkipNow()
	}
}

// TestMain runs the suite once per selected database (Nanostack: testenv replaces
// upstream's PG_SOURCE switch and externally prepared databases), then once per driver.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m, testenv.Suite{
		Name:    "postgres",
		Sources: []testenv.Source{testenv.Postgres14, testenv.Postgres18, testenv.Cockroach},
		Run:     runSuite,
		// Upstream ran CockroachDB without -tags postgres: its models have no range tables.
		DropTags: map[testenv.Source][]string{testenv.Cockroach: {"postgres"}},
	}))
}

func runSuite(env *testenv.Env) int {
	currentEnv = env
	testRoot = env.TestRoot
	source = Postgres
	if env.Source == testenv.Cockroach {
		source = CockroachDB
	}

	qrm.GlobalConfig.StrictScan = true

	// Nanostack: every driver runs, and the first failure is returned at the end, so a
	// failure with pq never hides the pgx results.
	code := 0
	for _, driverName := range pgDrivers() {

		fmt.Printf("\nRunning postgres tests for driver: %s, caching enabled: %t \n", driverName, withStatementCaching)

		ret := func() int {
			sqlDB, err := sql.Open(driverName, getConnectionString())
			if err != nil {
				fmt.Println(err.Error())
				panic("Failed to connect to test db")
			}
			db = stmtcache.New(sqlDB).SetCaching(withStatementCaching)
			defer func(db *stmtcache.DB) {
				err := db.Close()
				if err != nil {
					fmt.Printf("ERROR: Failed to close db connection, %v", err)
				}
			}(db)

			for i := 0; i < runCount(withStatementCaching); i++ {
				ret := env.M.Run()
				if ret != 0 {
					fmt.Printf("\nFAIL: Running postgres tests failed for driver: %s, caching enabled: %t \n", driverName, withStatementCaching)
					return ret
				}
			}
			return 0
		}()
		if ret != 0 && code == 0 {
			code = ret
		}
	}

	return code
}

// pgDrivers returns the drivers to run: NANODB_PG_DRIVERS (a comma-separated
// subset of postgres and pgx, so CI runs each driver in its own job) or both.
func pgDrivers() []string {
	all := []string{"postgres", "pgx"}
	value := strings.TrimSpace(os.Getenv("NANODB_PG_DRIVERS"))
	if value == "" {
		return all
	}
	var drivers []string
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(all, name) {
			panic(fmt.Sprintf("NANODB_PG_DRIVERS: unknown driver %q (want postgres, pgx)", name))
		}
		drivers = append(drivers, name)
	}
	return drivers
}

func runCount(stmtCaching bool) int {
	if stmtCaching {
		return 2
	}

	return 1
}

func getConnectionString() string {
	if sourceIsCockroachDB() {
		return dbconfig.CockroachConnectString
	}

	return dbconfig.PostgresConnectString
}

func allowUnusedColumns(f func()) {
	defer func() {
		qrm.GlobalConfig.StrictScan = true
	}()

	qrm.GlobalConfig.StrictScan = false

	f()
}

func useJsonUnmarshalFunc(unmarshalJson func(data []byte, v any) error, f func()) {
	defer func() {
		qrm.GlobalConfig.JsonUnmarshalFunc = json.Unmarshal
	}()

	qrm.GlobalConfig.JsonUnmarshalFunc = unmarshalJson

	f()
}

var loggedSQL string
var loggedSQLArgs []interface{}
var loggedDebugSQL string

var queryInfo postgres.QueryInfo
var callerFile string
var callerLine int
var callerFunction string

func init() {
	postgres.SetLogger(func(ctx context.Context, statement postgres.PrintableStatement) {
		loggedSQL, loggedSQLArgs = statement.Sql()
		loggedDebugSQL = statement.DebugSql()
	})

	postgres.SetQueryLogger(func(ctx context.Context, info postgres.QueryInfo) {
		queryInfo = info
		callerFile, callerLine, callerFunction = info.Caller()
	})
}

func requireLogged(t require.TestingT, statement postgres.Statement) {
	if _, ok := t.(*testing.B); ok {
		return // skip assert for benchmarks
	}

	query, args := statement.Sql()
	require.Equal(t, loggedSQL, query)
	require.Equal(t, loggedSQLArgs, args)
	require.Equal(t, loggedDebugSQL, statement.DebugSql())
}

func requireQueryLogged(t require.TestingT, statement postgres.Statement, rowsProcessed int64) {
	if _, ok := t.(*testing.B); ok {
		return // skip assert for benchmarks
	}

	query, args := statement.Sql()
	queryLogged, argsLogged := queryInfo.Statement.Sql()

	require.Equal(t, query, queryLogged)
	require.Equal(t, args, argsLogged)
	require.Equal(t, queryInfo.RowsProcessed, rowsProcessed)

	pc, file, _, _ := runtime.Caller(1)
	funcDetails := runtime.FuncForPC(pc)
	require.Equal(t, file, callerFile)
	require.NotEmpty(t, callerLine)
	require.Equal(t, funcDetails.Name(), callerFunction)
}

func skipForPgxDriver(t *testing.T) {
	if isPgxDriver() {
		t.SkipNow()
	}
}

func isPgxDriver() bool {
	switch db.Driver().(type) {
	case *stdlib.Driver:
		return true
	}

	return false
}
