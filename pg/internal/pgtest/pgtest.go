package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq" // Registers the postgres driver.
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// DefaultImage is the server image started when PG_TEST_IMAGE is empty.
	DefaultImage = "postgres:17-alpine"

	startTimeout = 2 * time.Minute
	password     = "pgtest"
)

//nolint:gochecknoglobals // One server per test binary is the point of the package.
var server = sync.OnceValues(start)

// Database is one test database.
type Database struct {
	// DSN is the URL of the database.
	DSN string
	// Host, Port, User, Password and Name are the parts of DSN.
	Host     string
	Port     int
	User     string
	Password string
	Name     string
}

// Fresh creates a database for the test and drops it when the test ends. The
// test fails when the server cannot start.
func Fresh(t testing.TB) Database {
	t.Helper()
	adminDSN, err := server()
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("pgtest: open admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := "pgtest_" + strings.ToLower(rand.Text()[:12])
	if _, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, dropErr := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); dropErr != nil {
			t.Errorf("pgtest: drop database %s: %v", name, dropErr)
		}
	})
	return parse(t, adminDSN, name)
}

// Open opens db and closes it when the test ends.
func Open(t testing.TB, db Database) *sql.DB {
	t.Helper()
	conn, err := sql.Open("postgres", db.DSN)
	if err != nil {
		t.Fatalf("pgtest: open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func parse(t testing.TB, adminDSN, name string) Database {
	t.Helper()
	parsed, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("pgtest: parse dsn: %v", err)
	}
	parsed.Path = "/" + name
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("pgtest: port of %s: %v", parsed.Redacted(), err)
	}
	pass, _ := parsed.User.Password()
	return Database{
		DSN:      parsed.String(),
		Host:     parsed.Hostname(),
		Port:     port,
		User:     parsed.User.Username(),
		Password: pass,
		Name:     name,
	}
}

func start() (string, error) {
	if dsn := os.Getenv("PG_TEST_DSN"); dsn != "" {
		return dsn, ping(dsn)
	}
	image := os.Getenv("PG_TEST_IMAGE")
	if image == "" {
		image = DefaultImage
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Image:        image,
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_PASSWORD": password},
		// The entrypoint starts the server twice: once to initialize, then for
		// real. The second ready line is the one to wait for.
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(startTimeout),
		Started: true,
	})
	if err != nil {
		return "", fmt.Errorf("start %s (is Docker running? set PG_TEST_DSN to use your own server): %w", image, err)
	}
	endpoint, err := container.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		return "", fmt.Errorf("read the address of %s: %w", image, err)
	}
	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("postgres", password),
		Host:     endpoint,
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}).String()
	return dsn, ping(dsn)
}

func ping(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}
