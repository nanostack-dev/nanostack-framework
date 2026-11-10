//go:build integration

package torture

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	gendb "github.com/nanostack-dev/nanostack-framework/gen/db"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

// These tests run every derived method of the torture fixture on
// PostgreSQL: a container of the fixture's image, or the server
// NANO_GEN_DATABASE_URL names. Run them with:
//
//	go test -tags integration ./repo/internal/torture/

// shop is the declaration of the fixture, as a service would write it.
var shop = project.Postgres{Name: "torture", Out: "db"} //nolint:gochecknoglobals // fixture

var server = sync.OnceValue(func() *gendb.Server { //nolint:gochecknoglobals // one server per test binary
	return gendb.NewServer(os.Getenv(gendb.EnvDatabaseURL), gendb.WithDefaults(shop).Image)
})

func TestMain(m *testing.M) {
	code := m.Run()
	server().Close()
	os.Exit(code)
}

// The committed go-jet code of the fixture is what its migrations produce.
func TestSchemaIsCurrent(t *testing.T) {
	err := gendb.Run(gendb.Options{Dir: ".", Postgres: shop, Fresh: true, Check: true, Database: server().Open})
	if err != nil {
		t.Fatalf("%v\nregenerate with, from gen/: go run ./cmd/nanostack gen db -dir repo/internal/torture "+
			"-name torture -out db, then go test ./repo/ -run TestTortureBaseIsCurrent -update", err)
	}
}

// openTorture returns a migrated scratch database.
func openTorture(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, drop, err := server().Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(drop)
	cfg := nanomigrate.DefaultConfig()
	cfg.BasePath = "migrations"
	cfg.Validate = false
	if err = nanomigrate.NewMigrator(cfg, db, slog.New(slog.DiscardHandler)).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustExec(t *testing.T, db execer, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
