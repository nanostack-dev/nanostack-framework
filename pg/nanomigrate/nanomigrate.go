package nanomigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file" // Registers the file source.

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// MigrationsTable is the version table, the same as modules/migrations.
const MigrationsTable = "schema_migrations"

// ErrStopped is returned when the context ended before the last migration
// ran. The migration in progress finished, so schema_migrations is clean.
// Migrate returns it only when no migration failed and a migration is still
// pending: a migration that fails while the context ends is returned as that
// failure, and a run that finished every migration before the stop took
// effect returns nil.
var ErrStopped = errors.New("nanomigrate: stopped before the last migration ran")

// Module provides *Migrator and runs it before the App starts.
var Module = nano.Module("migrations", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewMigrator),
	nano.Invoke(Run),
)

// Config selects the migrations. The yaml tags are the keys of the deprecated
// modules/migrations section, so one YAML file serves both.
type Config struct {
	// Enabled turns the migrations on.
	Enabled bool `config:"enabled" default:"true" json:"enabled" yaml:"enabled"`
	// BasePath is the directory of the migration files.
	BasePath string `config:"base_path" default:"migrations" json:"base_path" yaml:"basePath"`
	// Validate logs a warning for each module migration without a down file.
	Validate bool `config:"validate" default:"true" json:"validate" yaml:"validate"`
	// AutoFixDirty forces a dirty version to clean before migrating. Use it
	// only when the failed migration is known to have rolled back.
	AutoFixDirty bool `config:"auto_fix_dirty" default:"false" json:"auto_fix_dirty" yaml:"autoFixDirty"`
	// ForceVersion, when not zero, sets the version without running any
	// migration, for a manual repair.
	ForceVersion uint `config:"force_version,optional" json:"force_version" yaml:"forceVersion"`
}

// Migrator applies the migrations of one Config to one database.
type Migrator struct {
	db     *sql.DB
	config Config
	logger *slog.Logger
}

// NewMigrator returns the migrator. It does not touch the database.
func NewMigrator(cfg Config, db *sql.DB, logger *slog.Logger) *Migrator {
	return &Migrator{db: db, config: cfg, logger: logger.With(slog.String(log.KeyComponent, "migrations"))}
}

// Run validates and applies the migrations. List it with nano.Invoke.
func Run(ctx context.Context, m *Migrator) error {
	m.logger.InfoContext(ctx, "initializing database migrations")
	if err := m.Validate(ctx); err != nil {
		return fmt.Errorf("nanomigrate: validate: %w", err)
	}
	return m.Migrate(ctx)
}

// Migrate applies the pending migrations. When ctx ends during the run, the
// migration in progress finishes and Migrate returns ErrStopped.
func (m *Migrator) Migrate(ctx context.Context) error {
	if !m.config.Enabled {
		m.logger.InfoContext(ctx, "migrations are disabled")
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrStopped, context.Cause(ctx))
	}
	if err := requireDir(m.config.BasePath); err != nil {
		return err
	}
	// golang-migrate keeps the connection it migrates on until it is closed.
	// Migrate takes that connection itself and gives it back to the pool at
	// the end; Close of the golang-migrate driver would also close m.db, the
	// application's pool. The setup does not stop when ctx ends: only the
	// graceful stop of up does.
	conn, err := m.db.Conn(context.WithoutCancel(ctx))
	if err != nil {
		return fmt.Errorf("nanomigrate: take a connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	migrator, err := m.open(ctx, conn)
	if err != nil || migrator == nil {
		return err
	}
	version, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("nanomigrate: read version: %w", err)
	}
	if dirty {
		if err = m.fixDirty(migrator, version); err != nil {
			return err
		}
	}
	if m.config.ForceVersion > 0 {
		return force(migrator, m.config.ForceVersion)
	}
	return m.up(ctx, migrator, errors.Is(err, migrate.ErrNilVersion))
}

// open returns the golang-migrate instance on conn, or nil when the directory
// holds no migration file.
func (m *Migrator) open(ctx context.Context, conn *sql.Conn) (*migrate.Migrate, error) {
	driver, err := postgres.WithConnection(
		context.WithoutCancel(ctx), conn, &postgres.Config{MigrationsTable: MigrationsTable},
	)
	if err != nil {
		return nil, fmt.Errorf("nanomigrate: create driver: %w", err)
	}
	migrator, err := migrate.NewWithDatabaseInstance("file://"+m.config.BasePath, "postgres", driver)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.logger.InfoContext(ctx, "no migration files found", slog.String("path", m.config.BasePath))
			return nil, nil
		}
		return nil, fmt.Errorf("nanomigrate: open %s: %w", m.config.BasePath, err)
	}
	migrator.Log = &migrateLogger{logger: m.logger}
	return migrator, nil
}

func (m *Migrator) fixDirty(migrator *migrate.Migrate, version uint) error {
	if !m.config.AutoFixDirty {
		return fmt.Errorf("nanomigrate: dirty migration state at version %d", version)
	}
	return force(migrator, version)
}

func force(migrator *migrate.Migrate, version uint) error {
	if version > math.MaxInt {
		return fmt.Errorf("nanomigrate: version %d is too large", version)
	}
	if err := migrator.Force(int(version)); err != nil {
		return fmt.Errorf("nanomigrate: force version %d: %w", version, err)
	}
	return nil
}

// up runs Up and asks golang-migrate to stop after the migration in progress
// when ctx ends, so a stopped process records a clean version instead of a
// dirty one. fresh reports a database without a recorded version: there, a
// directory without migration files is not an error.
func (m *Migrator) up(ctx context.Context, migrator *migrate.Migrate, fresh bool) error {
	startedAt := time.Now()
	stopped, err := runUntilStopped(ctx, migrator.Up, func() {
		m.logger.WarnContext(ctx, "stopping after the migration in progress",
			slog.String(log.KeyCause, context.Cause(ctx).Error()))
		migrator.GracefulStop <- true
	})
	noFiles := fresh && errors.Is(err, os.ErrNotExist)
	if stopped {
		upErr := err
		if noFiles {
			upErr = nil
		}
		pending := func() (bool, error) { return m.pending(migrator) }
		if stopErr := stopOutcome(upErr, pending, context.Cause(ctx)); stopErr != nil {
			return stopErr
		}
		// Every migration ran before the stop took effect: report the run.
	}
	if errors.Is(err, migrate.ErrNoChange) {
		m.logger.InfoContext(ctx, "no migration changes needed")
		return nil
	}
	if noFiles {
		m.logger.InfoContext(ctx, "no migration files found", slog.String("path", m.config.BasePath))
		return nil
	}
	if err != nil {
		return fmt.Errorf("nanomigrate: migrate: %w", err)
	}
	// The per-step lines of golang-migrate are Debug. This Info line records
	// the version each environment landed on.
	version, dirty, err := migrator.Version()
	if err != nil {
		m.logger.InfoContext(ctx, "database migrations applied; failed to read resulting version", log.Err(err))
		return nil
	}
	m.logger.InfoContext(
		ctx,
		"database migrations applied",
		slog.Uint64(
			"version",
			uint64(version),
		),
		slog.Bool("dirty", dirty),
		slog.Duration("took", time.Since(startedAt)),
	)
	return nil
}

// runUntilStopped runs up, and calls stop once if ctx ends before up returns.
// It reports whether stop was called, and returns the error of up. It returns only
// after its watcher goroutine has ended, so stopped is exact: stop can make up
// return before stop itself returns (golang-migrate reads GracefulStop at
// once, and Up returns nil after a graceful stop), and a check that did not
// wait for the watcher could miss the stop and report a stopped run as
// complete.
func runUntilStopped(ctx context.Context, up func() error, stop func()) (stopped bool, err error) {
	runDone := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			stop()
			stopped = true
		case <-runDone:
		}
	}()
	err = up()
	close(runDone)
	<-watcherDone
	return stopped, err
}

// stopOutcome is what up returns when ctx ended while Up ran. upErr is the
// result of Up, pending reports whether a migration is still to run, and
// cause is the cause of ctx. Up also returns nil after a graceful stop, so
// only pending tells a stopped run from a complete one. A failed migration is
// returned as the failure, without ErrStopped and without the cancellation
// (errors.Is(err, context.Canceled) is false): schema_migrations may be
// dirty, and the caller must not take the error for a clean shutdown.
func stopOutcome(upErr error, pending func() (bool, error), cause error) error {
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		return fmt.Errorf("nanomigrate: migrate (stop requested: %s): %w", cause.Error(), upErr)
	}
	left, err := pending()
	if err != nil {
		return fmt.Errorf("nanomigrate: stop requested (%s); read the pending migrations: %w", cause.Error(), err)
	}
	if left {
		return fmt.Errorf("%w: %w", ErrStopped, cause)
	}
	return nil
}

// pending reports whether the directory holds a migration after the version
// that migrator records.
func (m *Migrator) pending(migrator *migrate.Migrate) (bool, error) {
	version, _, err := migrator.Version()
	recorded := err == nil
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return false, fmt.Errorf("read version: %w", err)
	}
	src, err := source.Open("file://" + m.config.BasePath)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", m.config.BasePath, err)
	}
	defer func() { _ = src.Close() }()
	if recorded {
		_, err = src.Next(version)
	} else {
		_, err = src.First()
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the next migration: %w", err)
	}
	return true, nil
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("nanomigrate: base path %q does not exist", path)
	}
	if err != nil {
		return fmt.Errorf("nanomigrate: stat base path %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("nanomigrate: base path %q is not a directory", path)
	}
	return nil
}

// migrateLogger writes the golang-migrate step lines at Debug.
type migrateLogger struct {
	logger *slog.Logger
}

func (l *migrateLogger) Printf(format string, v ...any) {
	l.logger.Debug("migrate", slog.String("detail", fmt.Sprintf(format, v...)))
}

func (l *migrateLogger) Verbose() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}
