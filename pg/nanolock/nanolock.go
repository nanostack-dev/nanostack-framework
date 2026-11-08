package nanolock

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"cirello.io/pglock"
	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module provides *pglock.Client.
var Module = nano.Module("pglock", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewClient),
)

// The tag defaults of the lease, which NewClient also applies to a zero
// duration.
const (
	defaultLeaseDuration      = 10 * time.Second
	defaultHeartbeatFrequency = time.Second
)

// Config is the lock table and the lease. The defaults are those of the
// deprecated modules/pglock. The yaml tags are the legacy keys of that fx
// module, for the deprecated fx loader (modules/config); core/config and the
// generated Bind read only the config: names.
//
// Durations default when zero. Start a Config built by hand from
// DefaultConfig so the boolean defaults apply too.
type Config struct {
	// TableName is the lock table.
	TableName string `config:"table_name" default:"pglock_locks" json:"table_name" yaml:"tableName"`
	// AutoCreateTable creates the table when it does not exist.
	AutoCreateTable bool `config:"auto_create_table" default:"true" json:"auto_create_table" yaml:"autoCreateTable"`
	// LeaseDuration is how long a lock stays held without a heartbeat.
	LeaseDuration time.Duration `config:"lease_duration" default:"10s" json:"lease_duration" yaml:"leaseDuration"`
	// HeartbeatFrequency is how often a held lock renews its lease. Keep it
	// well below LeaseDuration.
	HeartbeatFrequency time.Duration `config:"heartbeat_frequency" default:"1s" json:"heartbeat_frequency" yaml:"heartbeatFrequency"`
	// TestOnStartup acquires and releases a random lock while the App is
	// built, so a missing table or permission fails the start, not the
	// first lock.
	TestOnStartup bool `config:"test_on_startup" default:"true" json:"test_on_startup" yaml:"testOnStartup"`
}

// NewClient returns the lock client over db. It creates the table when
// AutoCreateTable is set and runs the startup test when TestOnStartup is set.
//
// A zero LeaseDuration or HeartbeatFrequency takes its default. A negative
// one fails. A lease shorter than two heartbeats fails with an error that
// wraps pglock.ErrDurationTooSmall. In both cases db stays open: pglock
// itself closes the pool on that error, so NewClient checks first.
func NewClient(ctx context.Context, cfg Config, db *sql.DB, logger *slog.Logger) (*pglock.Client, error) {
	logger = logger.With(slog.String(log.KeyComponent, "pglock"))
	// pglock reads a negative heartbeat as "no heartbeat" and a negative lease
	// as a timeout that has already passed, so refuse both.
	if cfg.LeaseDuration < 0 || cfg.HeartbeatFrequency < 0 {
		return nil, fmt.Errorf("nanolock: lease_duration %s and heartbeat_frequency %s must not be negative",
			cfg.LeaseDuration, cfg.HeartbeatFrequency)
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.HeartbeatFrequency == 0 {
		cfg.HeartbeatFrequency = defaultHeartbeatFrequency
	}
	if cfg.HeartbeatFrequency > 0 && cfg.LeaseDuration < 2*cfg.HeartbeatFrequency {
		return nil, fmt.Errorf("nanolock: lease_duration %s must be at least twice heartbeat_frequency %s: %w",
			cfg.LeaseDuration, cfg.HeartbeatFrequency, pglock.ErrDurationTooSmall)
	}
	options := []pglock.ClientOption{
		pglock.WithLeaseDuration(cfg.LeaseDuration),
		pglock.WithHeartbeatFrequency(cfg.HeartbeatFrequency),
	}
	if cfg.TableName != "" {
		options = append(options, pglock.WithCustomTable(cfg.TableName))
	}
	// UnsafeNew, not New: New accepts only a *pq.Driver pool, and the pool of
	// pg/nanopg is lib/pq wrapped by otelsql.
	client, err := pglock.UnsafeNew(db, options...)
	if err != nil {
		return nil, fmt.Errorf("nanolock: create client: %w", err)
	}
	if cfg.AutoCreateTable {
		if err = client.TryCreateTable(); err != nil {
			return nil, fmt.Errorf("nanolock: create table %s: %w", cfg.TableName, err)
		}
	}
	if cfg.TestOnStartup {
		lock, acquireErr := client.AcquireContext(ctx, "nanolock-startup-"+uuid.NewString(), pglock.FailIfLocked())
		if acquireErr != nil {
			return nil, fmt.Errorf("nanolock: startup test lock: %w", acquireErr)
		}
		if closeErr := lock.Close(); closeErr != nil {
			logger.WarnContext(ctx, "error releasing the startup test lock", log.Err(closeErr))
		}
	}
	return client, nil
}

// DefaultConfig returns the Config of the default: tags. Start a Config built
// by hand, in a test or an Override, from it, so the boolean defaults
// (AutoCreateTable, TestOnStartup) apply too.
func DefaultConfig() Config {
	return Config{
		TableName:          "pglock_locks",
		AutoCreateTable:    true,
		LeaseDuration:      defaultLeaseDuration,
		HeartbeatFrequency: defaultHeartbeatFrequency,
		TestOnStartup:      true,
	}
}
