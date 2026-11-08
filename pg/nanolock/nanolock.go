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

// Config is the lock table and the lease. The defaults and yaml tags are those
// of the deprecated modules/pglock, so one YAML file serves both.
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
func NewClient(ctx context.Context, cfg Config, db *sql.DB, logger *slog.Logger) (*pglock.Client, error) {
	logger = logger.With(slog.String(log.KeyComponent, "pglock"))
	options := []pglock.ClientOption{
		pglock.WithLeaseDuration(cfg.LeaseDuration),
		pglock.WithHeartbeatFrequency(cfg.HeartbeatFrequency),
	}
	if cfg.TableName != "" {
		options = append(options, pglock.WithCustomTable(cfg.TableName))
	}
	client, err := pglock.New(db, options...)
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
