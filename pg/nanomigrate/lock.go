package nanomigrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4/database"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// The backoff of the migration lock poll.
const (
	lockPollFirst = 50 * time.Millisecond
	lockPollMax   = 2 * time.Second
	unlockTimeout = 5 * time.Second
)

// migrationLock is the session advisory lock that golang-migrate takes on
// the migration connection, held one level above golang-migrate's own.
type migrationLock struct {
	conn   *sql.Conn
	id     string
	logger *slog.Logger
}

// lock takes the migration advisory lock on conn, waiting while another
// instance holds it, until ctx ends. golang-migrate takes the same lock with
// context.Background, so a stop could not interrupt its wait. Session
// advisory locks are re-entrant: once lock holds it, the Lock of
// golang-migrate on the same connection stacks and returns at once, and its
// Unlock pops only its own level.
//
// The id is golang-migrate's: GenerateAdvisoryLockId of current_database(),
// current_schema() and MigrationsTable, read on conn as golang-migrate reads
// them.
func (m *Migrator) lock(ctx context.Context, conn *sql.Conn) (*migrationLock, error) {
	var dbName, schema sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT current_database(), current_schema()").
		Scan(&dbName, &schema); err != nil {
		return nil, m.lockFailure(ctx, "read the database and schema names", err)
	}
	if !dbName.Valid || !schema.Valid || dbName.String == "" {
		return nil, fmt.Errorf("nanomigrate: no current database or schema for the migration lock")
	}
	id, err := database.GenerateAdvisoryLockId(dbName.String, schema.String, MigrationsTable)
	if err != nil {
		return nil, fmt.Errorf("nanomigrate: migration lock id: %w", err)
	}
	wait, waited := lockPollFirst, false
	for {
		var held bool
		if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", id).Scan(&held); err != nil {
			return nil, m.lockFailure(ctx, "take the migration lock", err)
		}
		if held {
			if waited {
				m.logger.InfoContext(ctx, "migration lock acquired")
			}
			return &migrationLock{conn: conn, id: id, logger: m.logger}, nil
		}
		if !waited {
			m.logger.InfoContext(ctx, "waiting for the migration lock held by another instance")
			waited = true
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, stoppedError{cause: context.Cause(ctx)}
		case <-timer.C:
		}
		wait = min(2*wait, lockPollMax)
	}
}

// lockFailure is a failed lock query: a stop when ctx ended, else the
// error.
func (m *Migrator) lockFailure(ctx context.Context, what string, err error) error {
	if ctx.Err() != nil {
		return stoppedError{cause: context.Cause(ctx)}
	}
	return fmt.Errorf("nanomigrate: %s: %w", what, err)
}

// release pops the level that lock took. When the release cannot be
// confirmed, it discards the connection, so a session that may still hold
// the lock never goes back to the application's pool. Call it before
// conn.Close.
func (l *migrationLock) release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()
	var released bool
	err := l.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", l.id).Scan(&released)
	if err == nil && released {
		return
	}
	l.logger.WarnContext(ctx, "migration lock release not confirmed; discarding the connection", log.Err(err))
	discard(l.conn)
}

// discard closes conn's driver connection instead of returning it to the
// pool.
func discard(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}
