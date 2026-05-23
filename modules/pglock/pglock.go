package pglock

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	cpglock "cirello.io/pglock"
	"github.com/google/uuid"
	"github.com/nanostack-dev/nanostack-framework/modules/config"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
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

func NewClient(lc fx.Lifecycle, log zerolog.Logger, db *sql.DB, cfg Config) (*cpglock.Client, error) {
	logger := log.With().Str("component", "pglock").Logger()
	options := []cpglock.ClientOption{
		cpglock.WithLeaseDuration(cfg.LeaseDuration),
		cpglock.WithHeartbeatFrequency(cfg.HeartbeatFrequency),
	}
	if cfg.TableName != "" {
		options = append(options, cpglock.WithCustomTable(cfg.TableName))
	}
	client, err := cpglock.New(db, options...)
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
	cfg := Config{TableName: "pglock_locks", AutoCreateTable: true, LeaseDuration: defaultLeaseDuration, HeartbeatFrequency: defaultHeartbeatFrequency, TestOnStartup: true}
	_ = loader.LoadConfig("pglock", &cfg)
	return cfg, nil
}
