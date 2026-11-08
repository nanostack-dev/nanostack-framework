package pglock

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	cpglock "cirello.io/pglock"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/modules/config"
)

const (
	defaultLeaseDuration      = 10 * time.Second
	defaultHeartbeatFrequency = 1 * time.Second
)

type Config struct {
	TableName          string        `yaml:"tableName"`
	AutoCreateTable    bool          `yaml:"autoCreateTable"`
	LeaseDuration      time.Duration `yaml:"leaseDuration"`
	HeartbeatFrequency time.Duration `yaml:"heartbeatFrequency"`
	TestOnStartup      bool          `yaml:"testOnStartup"`
}

// NewClient returns the lock client over db. A zero duration takes its
// default. A negative duration, or a lease shorter than two heartbeats, fails
// before pglock sees db, which pglock would close on that error.
func NewClient(lc fx.Lifecycle, log zerolog.Logger, db *sql.DB, cfg Config) (*cpglock.Client, error) {
	logger := log.With().Str("component", "pglock").Logger()
	// pglock reads a negative heartbeat as "no heartbeat" and a negative lease
	// as a timeout that has already passed, so refuse both.
	if cfg.LeaseDuration < 0 || cfg.HeartbeatFrequency < 0 {
		return nil, fmt.Errorf("pglock: leaseDuration %s and heartbeatFrequency %s must not be negative",
			cfg.LeaseDuration, cfg.HeartbeatFrequency)
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.HeartbeatFrequency == 0 {
		cfg.HeartbeatFrequency = defaultHeartbeatFrequency
	}
	if cfg.HeartbeatFrequency > 0 && cfg.LeaseDuration < 2*cfg.HeartbeatFrequency {
		return nil, fmt.Errorf("pglock: leaseDuration %s must be at least twice heartbeatFrequency %s: %w",
			cfg.LeaseDuration, cfg.HeartbeatFrequency, cpglock.ErrDurationTooSmall)
	}
	options := []cpglock.ClientOption{
		cpglock.WithLeaseDuration(cfg.LeaseDuration),
		cpglock.WithHeartbeatFrequency(cfg.HeartbeatFrequency),
	}
	if cfg.TableName != "" {
		options = append(options, cpglock.WithCustomTable(cfg.TableName))
	}
	// UnsafeNew, not New: New accepts only a bare *pq.Driver pool, and the
	// pool of modules/postgres may be wrapped by otelsql.
	client, err := cpglock.UnsafeNew(db, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create pglock client: %w", err)
	}
	if cfg.AutoCreateTable {
		if err := client.TryCreateTable(); err != nil {
			return nil, fmt.Errorf("failed to create pglock table: %w", err)
		}
	}
	if cfg.TestOnStartup {
		lock, err := client.Acquire(uuid.NewString(), cpglock.FailIfLocked())
		if err != nil {
			return nil, fmt.Errorf("test lock acquisition failed: %w", err)
		}
		if err := lock.Close(); err != nil {
			logger.Warn().Err(err).Msg("error releasing test lock")
		}
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return nil }})
	return client, nil
}

func ProvideConfig(loader config.Loader) (Config, error) {
	cfg := Config{
		TableName:          "pglock_locks",
		AutoCreateTable:    true,
		LeaseDuration:      defaultLeaseDuration,
		HeartbeatFrequency: defaultHeartbeatFrequency,
		TestOnStartup:      true,
	}
	_ = loader.LoadConfig("pglock", &cfg)
	return cfg, nil
}
