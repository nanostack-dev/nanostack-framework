// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jetmysql "github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/stmtcache"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"runtime"
	"sync"

	"os"
	"testing"
)

var db *stmtcache.DB

var source string
var withStatementCaching bool
var testRoot string

// currentEnv is the testenv run in progress (Nanostack harness).
var currentEnv *testenv.Env

const MariaDB = "MariaDB"

func init() {
	withStatementCaching = os.Getenv(testenv.StmtCacheEnv) == "true"
}

func sourceIsMariaDB() bool {
	return source == MariaDB
}

// TestMain runs the suite once per selected database (Nanostack: testenv replaces
// upstream's MY_SQL_SOURCE switch and externally prepared databases).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m, testenv.Suite{
		Name:    "mysql",
		Sources: []testenv.Source{testenv.MySQL, testenv.MariaDB},
		Run:     runSuite,
	}))
}

func runSuite(env *testenv.Env) int {
	currentEnv = env
	testRoot = env.TestRoot
	source = ""
	if env.Source == testenv.MariaDB {
		source = MariaDB
	}

	fmt.Printf("\nRunning mysql tests caching enabled: %t \n", withStatementCaching)

	sqlDB, err := sql.Open("mysql", dbconfig.MySQLConnectionString(sourceIsMariaDB(), ""))
	if err != nil {
		panic("Failed to connect to test db" + err.Error())
	}

	db = stmtcache.New(sqlDB).SetCaching(withStatementCaching)
	defer db.Close()

	for i := 0; i < runCount(withStatementCaching); i++ {
		ret := env.M.Run()
		if ret != 0 {
			fmt.Printf("\nFAIL: Running mysql tests failed, caching enabled: %t \n", withStatementCaching)
			return ret
		}
	}

	return 0
}

func getConnectionString() string {
	return dbconfig.MySQLConnectionString(sourceIsMariaDB(), "")
}

func runCount(stmtCaching bool) int {
	if stmtCaching {
		return 3
	}

	return 1
}

// loggerMu guards the logged values below: parallel subtests (the keyset
// walks) run queries at the same time. Nanostack-only, see db/UPSTREAM.md.
var loggerMu sync.Mutex

var loggedSQL string
var loggedSQLArgs []interface{}
var loggedDebugSQL string

var queryInfo jetmysql.QueryInfo
var callerFile string
var callerLine int
var callerFunction string

func init() {
	jetmysql.SetLogger(func(ctx context.Context, statement jetmysql.PrintableStatement) {
		loggerMu.Lock()
		defer loggerMu.Unlock()
		loggedSQL, loggedSQLArgs = statement.Sql()
		loggedDebugSQL = statement.DebugSql()
	})

	jetmysql.SetQueryLogger(func(ctx context.Context, info jetmysql.QueryInfo) {
		loggerMu.Lock()
		defer loggerMu.Unlock()
		queryInfo = info
		callerFile, callerLine, callerFunction = info.Caller()
	})
}

func requireLogged(t *testing.T, statement mysql.Statement) {
	query, args := statement.Sql()
	require.Equal(t, loggedSQL, query)
	require.Equal(t, loggedSQLArgs, args)
	require.Equal(t, loggedDebugSQL, statement.DebugSql())
}

func requireQueryLogged(t *testing.T, statement mysql.Statement, rowsProcessed int64) {
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

func skipForMariaDB(t *testing.T) {
	if sourceIsMariaDB() {
		t.SkipNow()
	}
}

func onlyMariaDB(t *testing.T) {
	if !sourceIsMariaDB() {
		t.SkipNow()
	}
}
