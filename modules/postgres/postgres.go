package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/modules/config"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"

	_ "github.com/lib/pq" // Required for postgres driver registration.
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const (
	defaultMaxConnections     = 25
	defaultMaxIdleConnections = 25
	connectionMaxLifetime     = 5 * time.Minute
	pingTimeout               = 5 * time.Second
)

type Config struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode,omitempty"`
}

func (c Config) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
}

type Params struct {
	fx.In
	Lifecycle fx.Lifecycle
	Logger    zerolog.Logger
	Loader    config.Loader
	Registry  *health.Registry `optional:"true"`
}

func NewConnection(params Params) (*sql.DB, error) {
	var cfg Config
	if err := params.Loader.LoadConfig("postgres", &cfg); err != nil {
		params.Logger.Error().Err(err).Msg("failed to load postgres config")
		return nil, fmt.Errorf("database config loading failed: %w", err)
	}
	params.Logger.Info().Str("host", cfg.Host).Int("port", cfg.Port).Str("db", cfg.DBName).Msg("connecting to postgres")
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(defaultMaxConnections)
	db.SetMaxIdleConns(defaultMaxIdleConnections)
	db.SetConnMaxLifetime(connectionMaxLifetime)
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	params.Lifecycle.Append(fx.Hook{OnStop: func(context.Context) error {
		params.Logger.Info().Msg("closing postgres connection pool")
		return db.Close()
	}})

	if params.Registry != nil {
		params.Registry.Register(health.NewChecker("postgres", func(ctx context.Context) error {
			return db.PingContext(ctx)
		}))
	}

	return db, nil
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"postgres",
	fx.Provide(NewConnection),
)
