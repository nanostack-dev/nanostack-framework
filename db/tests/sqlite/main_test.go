// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/internal/utils/throw"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
	"github.com/nanostack-dev/nanostack-framework/db/stmtcache"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

var db *stmtcache.DB
var sampleDB *stmtcache.DB

var withStatementCaching bool

// currentEnv is the testenv run in progress (Nanostack harness).
var currentEnv *testenv.Env

func init() {
	withStatementCaching = os.Getenv(testenv.StmtCacheEnv) == "true"
}

// TestMain runs the suite against copies of the SQLite test databases (Nanostack:
// testenv fetches the test data and copies the files, so runs never share writes).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m, testenv.Suite{
		Name:    "sqlite",
		Sources: []testenv.Source{testenv.SQLite},
		Run:     runSuite,
	}))
}

func runSuite(env *testenv.Env) int {
	currentEnv = env

	fmt.Printf("\nRunning sqlite tests caching enabled: %t \n", withStatementCaching)

	sqlDB, err := sql.Open("sqlite3", "file:"+dbconfig.SakilaDBPath)
	throw.OnError(err)
	db = stmtcache.New(sqlDB).SetCaching(withStatementCaching)
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' as 'chinook';", dbconfig.ChinookDBPath))
	throw.OnError(err)

	sqlSampleDB, err := sql.Open("sqlite3", dbconfig.TestSampleDBPath)
	throw.OnError(err)
	sampleDB = stmtcache.New(sqlSampleDB).SetCaching(withStatementCaching)
	defer sampleDB.Close()

	for i := 0; i < runCount(withStatementCaching); i++ {
		ret := env.M.Run()
		if ret != 0 {
			fmt.Printf("\nFAIL: Running sqlite tests failed, caching enabled: %t \n", withStatementCaching)
			return ret
		}
	}

	return 0
}

func allowUnmappedFields(f func()) {
	previous := qrm.GlobalConfig.StrictFieldMapping
	defer func() { qrm.GlobalConfig.StrictFieldMapping = previous }()
	qrm.GlobalConfig.StrictFieldMapping = false
	f()
}

func requireStrictFieldMapping(f func()) {
	previous := qrm.GlobalConfig.StrictFieldMapping
	defer func() { qrm.GlobalConfig.StrictFieldMapping = previous }()
	qrm.GlobalConfig.StrictFieldMapping = true
	f()
}

func runCount(stmtCaching bool) int {
	if stmtCaching {
		return 4
	}

	return 1
}

var loggedSQL string
var loggedSQLArgs []interface{}
var loggedDebugSQL string

var queryInfo sqlite.QueryInfo
var callerFile string
var callerLine int
var callerFunction string

func init() {
	sqlite.SetLogger(func(ctx context.Context, statement sqlite.PrintableStatement) {
		loggedSQL, loggedSQLArgs = statement.Sql()
		loggedDebugSQL = statement.DebugSql()
	})

	sqlite.SetQueryLogger(func(ctx context.Context, info sqlite.QueryInfo) {
		queryInfo = info
		callerFile, callerLine, callerFunction = info.Caller()
	})
}

func requireQueryLogged(t *testing.T, statement sqlite.Statement, rowsProcessed int64) {
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

func requireLogged(t *testing.T, statement sqlite.Statement) {
	query, args := statement.Sql()
	require.Equal(t, loggedSQL, query)
	require.Equal(t, loggedSQLArgs, args)
	require.Equal(t, loggedDebugSQL, statement.DebugSql())
}

func beginSampleDBTx(t *testing.T) *stmtcache.Tx {
	tx, err := sampleDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	return tx
}

func beginDBTx(t *testing.T) *stmtcache.Tx {
	tx, err := db.Begin()
	require.NoError(t, err)
	return tx
}
