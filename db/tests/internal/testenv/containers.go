package testenv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq" // CockroachDB data load
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/cockroachdb"
	"github.com/testcontainers/testcontainers-go/modules/mariadb"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Credentials and names the upstream suites expect (see dbconfig).
const (
	user     = "jet"
	password = "jet"
	pgDBName = "jetdb"
)

// startupTimeout bounds one container start, data load included. The dumps are ~13 MB.
const startupTimeout = 5 * time.Minute

// The PostgreSQL and CockroachDB schemas, in upstream init order.
var pgSchemas = []string{"northwind", "dvds", "test_sample", "chinook", "chinook2"}

// The MySQL and MariaDB databases. 00-settings.sql grants jet all privileges and sets sql_mode.
var mysqlDatabases = []string{"dvds", "dvds2", "test_sample"}

// Database is a running, loaded database server.
type Database struct {
	Source Source
	Host   string
	Port   int

	container testcontainers.Container
}

// Close stops and removes the container.
func (d *Database) Close() error {
	if d == nil || d.container == nil {
		return nil
	}
	return testcontainers.TerminateContainer(d.container)
}

// Start runs the container for s and loads the test data from dataDir.
func Start(ctx context.Context, s Source, dataDir string) (*Database, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	var (
		ctr  testcontainers.Container
		port string
		err  error
	)

	switch {
	case s.IsPostgres():
		ctr, err = startPostgres(ctx, s, dataDir)
		port = "5432/tcp"
	case s == Cockroach:
		ctr, err = startCockroach(ctx)
		port = "26257/tcp"
	case s == MySQL:
		ctr, err = startMySQL(ctx, dataDir)
		port = "3306/tcp"
	case s == MariaDB:
		ctr, err = startMariaDB(ctx, dataDir)
		port = "3306/tcp"
	default:
		return nil, fmt.Errorf("%s does not run in a container", s)
	}

	d := &Database{Source: s, container: ctr}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("start %s (%s): %w", s, images[s], err), d.Close())
	}

	if d.Host, err = ctr.Host(ctx); err != nil {
		return nil, errors.Join(err, d.Close())
	}
	mapped, err := ctr.MappedPort(ctx, port)
	if err != nil {
		return nil, errors.Join(err, d.Close())
	}
	d.Port = int(mapped.Num())

	if s == Cockroach {
		if err = loadCockroach(ctx, d.PostgresDSN(), dataDir); err != nil {
			return nil, errors.Join(fmt.Errorf("load %s: %w", s, err), d.Close())
		}
	}

	return d, nil
}

// PostgresDSN is the lib/pq connection string of a PostgreSQL or CockroachDB server,
// in the same form as dbconfig builds it.
func (d *Database) PostgresDSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, user, password, pgDBName)
}

func startPostgres(ctx context.Context, s Source, dataDir string) (testcontainers.Container, error) {
	scripts := make([]string, len(pgSchemas))
	for i, schema := range pgSchemas {
		scripts[i] = filepath.Join(dataDir, "init", "postgres", schema+".sql")
	}

	return postgres.Run(ctx, images[s],
		postgres.WithDatabase(pgDBName),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.WithOrderedInitScripts(scripts...),
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout,
			// The entrypoint restarts the server after running the init scripts.
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
}

func startCockroach(ctx context.Context) (testcontainers.Container, error) {
	// Insecure mode accepts the suites' password-bearing DSN without TLS.
	return cockroachdb.Run(ctx, images[Cockroach],
		cockroachdb.WithDatabase(pgDBName),
		cockroachdb.WithUser(user),
		cockroachdb.WithInsecure(),
	)
}

// loadCockroach runs the CockroachDB dumps through database/sql, as upstream's init
// program did: the image has no init-script hook that runs before the wait completes.
func loadCockroach(ctx context.Context, dsn, dataDir string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	for _, schema := range pgSchemas {
		name := filepath.Join(dataDir, "init", "cockroach", schema+".sql")
		script, readErr := os.ReadFile(name) // #nosec G304 -- test data
		if readErr != nil {
			return readErr
		}
		// CockroachDB can abort the load with a TransactionRetryError; upstream retries
		// 3 times. The dumps drop and recreate their schema, so a retry is safe.
		err = retry(3, func() error { return execScript(ctx, db, string(script)) })
		if err != nil {
			return fmt.Errorf("schema %s: %w", schema, err)
		}
	}
	return nil
}

// execScript runs script in one transaction, as upstream did: the dumps reference
// tables they create earlier in the same script. Since v25.1 CockroachDB commits the
// open transaction before each DDL statement unless autocommit_before_ddl is off.
func execScript(ctx context.Context, db *sql.DB, script string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err = conn.ExecContext(ctx, "SET autocommit_before_ddl = false"); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, script); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func retry(count int, f func() error) error {
	var err error
	for range count {
		if err = f(); err == nil {
			return nil
		}
	}
	return err
}

func mysqlScripts(dataDir string) []string {
	scripts := []string{filepath.Join(dataDir, "init", "mysql", "00-settings.sql")}
	for _, name := range mysqlDatabases {
		scripts = append(scripts, filepath.Join(dataDir, "init", "mysql", name+".sql"))
	}
	return scripts
}

// The dumps use DELIMITER, a mysql client command, so the image entrypoint loads them.
// Functions and triggers in dvds.sql need log_bin_trust_function_creators.
func startMySQL(ctx context.Context, dataDir string) (testcontainers.Container, error) {
	return mysql.Run(ctx, images[MySQL],
		mysql.WithUsername(user),
		mysql.WithPassword(password),
		mysql.WithDatabase(""), // upstream creates no default database
		mysql.WithScripts(mysqlScripts(dataDir)...),
		// The image sets no locale, so the entrypoint's mysql client would read the
		// dumps as latin1 and double-encode their UTF-8 comments.
		testcontainers.WithEnv(map[string]string{"LANG": "C.UTF-8"}),
		testcontainers.WithCmdArgs("--log_bin_trust_function_creators=1"),
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout,
			wait.ForLog("port: 3306  MySQL Community Server"),
			wait.ForListeningPort("3306/tcp"),
		),
	)
}

func startMariaDB(ctx context.Context, dataDir string) (testcontainers.Container, error) {
	return mariadb.Run(ctx, images[MariaDB],
		mariadb.WithUsername(user),
		mariadb.WithPassword(password),
		mariadb.WithDatabase(""),
		mariadb.WithScripts(mysqlScripts(dataDir)...),
		testcontainers.WithCmdArgs("--log_bin_trust_function_creators=1"),
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout,
			wait.ForLog("port: 3306  mariadb.org binary distribution"),
			wait.ForListeningPort("3306/tcp"),
		),
	)
}
