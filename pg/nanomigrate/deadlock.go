package nanomigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"regexp"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/lib/pq"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// A migration that Postgres aborts to break a deadlock is run again, up to
// deadlockRetries times, after a pause of deadlockPause times the attempt.
//
// The other side of such a deadlock is a session outside the migration: a
// TimescaleDB policy job, an autovacuum, a request of an older replica. One
// case: a TimescaleDB retention policy on a hypertable takes an
// AccessExclusiveLock on every table that the hypertable references, even
// without a chunk to drop. When it runs during CREATE INDEX CONCURRENTLY on
// one of those tables, each session waits for the other, and Postgres aborts
// the session whose deadlock_timeout ends first. That is a matter of timing,
// so the deadlock shows up as a rare startup flake.
const (
	deadlockRetries = 3
	deadlockPause   = 500 * time.Millisecond
)

// sqlStateDeadlock is the SQLSTATE of deadlock_detected.
const sqlStateDeadlock = "40P01"

// transactionControl matches a statement that ends or opens a transaction
// inside a migration file. Such a file can commit part of its work before it
// fails, so running it again is not safe. The match is loose on purpose: a
// false match only turns the retry off.
var transactionControl = regexp.MustCompile(
	`(?im)^\s*(BEGIN|COMMIT|END|ROLLBACK|START\s+TRANSACTION)\s*(TRANSACTION|WORK)?\s*;`,
)

// isDeadlock reports whether err is a migration that Postgres aborted as a
// deadlock victim. golang-migrate returns a database.Error, which has no
// Unwrap: the driver error is its OrigErr.
func isDeadlock(err error) bool {
	if hasSQLState(err, sqlStateDeadlock) {
		return true
	}
	if dbErr, ok := errors.AsType[database.Error](err); ok {
		return hasSQLState(dbErr.OrigErr, sqlStateDeadlock)
	}
	return false
}

func hasSQLState(err error, code string) bool {
	if pqErr, ok := errors.AsType[*pq.Error](err); ok {
		return string(pqErr.Code) == code
	}
	stated, ok := errors.AsType[interface {
		error
		SQLState() string
	}](err)
	return ok && stated.SQLState() == code
}

// invalidIndexes returns the OIDs of the indexes that Postgres marks invalid.
// A CREATE INDEX CONCURRENTLY that fails after its first phase leaves its
// index in the catalog, invalid.
func invalidIndexes(ctx context.Context, conn *sql.Conn) (map[uint32]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT c.oid, format('%I.%I', n.nspname, c.relname)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT i.indisvalid`)
	if err != nil {
		return nil, fmt.Errorf("nanomigrate: list invalid indexes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := map[uint32]string{}
	for rows.Next() {
		var oid uint32
		var name string
		if err = rows.Scan(&oid, &name); err != nil {
			return nil, fmt.Errorf("nanomigrate: list invalid indexes: %w", err)
		}
		found[oid] = name
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("nanomigrate: list invalid indexes: %w", err)
	}
	return found, nil
}

// upRetryingDeadlocks runs up, and runs the failed migration again when
// Postgres aborted it as a deadlock victim (see deadlockRetries).
func (m *Migrator) upRetryingDeadlocks(
	ctx context.Context, conn *sql.Conn, migrator *migrate.Migrate, fresh bool,
) error {
	for attempt := 1; ; attempt++ {
		before, err := invalidIndexes(ctx, conn)
		if err != nil {
			return err
		}
		err = m.up(ctx, migrator, fresh)
		if err == nil || !isDeadlock(err) || ctx.Err() != nil || attempt > deadlockRetries {
			return err
		}
		version, retry, why := m.retryable(migrator)
		if !retry {
			m.logger.WarnContext(ctx, "deadlock victim migration not retried", slog.String("reason", why))
			return err
		}
		m.logger.WarnContext(ctx, "migration aborted as a deadlock victim; running it again",
			slog.Uint64("version", uint64(version)), slog.Int("attempt", attempt), log.Err(err))
		if rerr := m.resetDeadlockVictim(ctx, conn, migrator, version, before); rerr != nil {
			return errors.Join(err, rerr)
		}
		timer := time.NewTimer(time.Duration(attempt) * deadlockPause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

// retryable reports the dirty version that a deadlock left, and whether its
// migration can run again: its file must hold no transaction control, so the
// abort rolled back all of it, or only a failed CREATE INDEX CONCURRENTLY
// remains (resetDeadlockVictim drops its invalid index).
func (m *Migrator) retryable(migrator *migrate.Migrate) (uint, bool, string) {
	version, dirty, err := migrator.Version()
	if err != nil {
		return 0, false, "read version: " + err.Error()
	}
	if !dirty {
		return version, false, "the recorded version is not dirty"
	}
	body, err := m.upBody(version)
	if err != nil {
		return version, false, err.Error()
	}
	if transactionControl.Match(body) {
		return version, false, "the migration file controls its own transactions"
	}
	return version, true, ""
}

// upBody reads the up file of version.
func (m *Migrator) upBody(version uint) ([]byte, error) {
	src, err := source.Open("file://" + m.config.BasePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", m.config.BasePath, err)
	}
	defer func() { _ = src.Close() }()
	r, _, err := src.ReadUp(version)
	if err != nil {
		return nil, fmt.Errorf("read the up migration %d: %w", version, err)
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}

// resetDeadlockVictim undoes what the aborted migration left: it drops each
// index that became invalid during the attempt, then records the version
// before the migration, clean. The abort rolled back the rest.
func (m *Migrator) resetDeadlockVictim(
	ctx context.Context, conn *sql.Conn, migrator *migrate.Migrate, version uint, before map[uint32]string,
) error {
	after, err := invalidIndexes(ctx, conn)
	if err != nil {
		return err
	}
	for oid, name := range after {
		if _, existed := before[oid]; existed {
			continue
		}
		m.logger.WarnContext(ctx, "dropping the invalid index of the aborted migration", slog.String("index", name))
		// name comes from format('%I.%I'), so it is quoted.
		if _, err = conn.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+name); err != nil {
			return fmt.Errorf("nanomigrate: drop the invalid index %s: %w", name, err)
		}
	}
	previous, err := previousVersion(m.config.BasePath, version)
	if err != nil {
		return err
	}
	if err = migrator.Force(previous); err != nil {
		return fmt.Errorf("nanomigrate: force version %d: %w", previous, err)
	}
	return nil
}

// previousVersion returns the version before version in the directory, or
// database.NilVersion when version is the first migration.
func previousVersion(basePath string, version uint) (int, error) {
	src, err := source.Open("file://" + basePath)
	if err != nil {
		return 0, fmt.Errorf("nanomigrate: open %s: %w", basePath, err)
	}
	defer func() { _ = src.Close() }()
	prev, err := src.Prev(version)
	if errors.Is(err, os.ErrNotExist) {
		return database.NilVersion, nil
	}
	if err != nil {
		return 0, fmt.Errorf("nanomigrate: the migration before %d: %w", version, err)
	}
	if prev > math.MaxInt {
		return 0, fmt.Errorf("nanomigrate: version %d is too large", prev)
	}
	return int(prev), nil
}
