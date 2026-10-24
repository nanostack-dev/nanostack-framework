package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/nanostack-dev/nanostack-framework/modules/config"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"

	"github.com/XSAM/otelsql"
	_ "github.com/lib/pq" // Required for postgres driver registration.
	"github.com/rs/zerolog"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
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
	// TracerProvider defaults to the global provider (see modules/tracing).
	TracerProvider trace.TracerProvider `optional:"true"`
}

func NewConnection(params Params) (*sql.DB, error) {
	var cfg Config
	if err := params.Loader.LoadConfig("postgres", &cfg); err != nil {
		params.Logger.Error().Err(err).Msg("failed to load postgres config")
		return nil, fmt.Errorf("database config loading failed: %w", err)
	}
	params.Logger.Info().Str("host", cfg.Host).Int("port", cfg.Port).Str("db", cfg.DBName).Msg("connecting to postgres")
	db, err := otelsql.Open("postgres", cfg.DSN(), traceOptions(params.TracerProvider)...)
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

// traceOptions makes queries child spans of the caller's trace. Span names are
// the database/sql method ("sql.conn.query"), the statement text is recorded
// without bind arguments, and pings, session resets and row iteration produce
// no spans. A query whose context carries no span — migrations, pollers,
// lock heartbeats — starts no trace of its own.
func traceOptions(provider trace.TracerProvider) []otelsql.Option {
	options := []otelsql.Option{
		otelsql.WithAttributes(semconv.DBSystemNamePostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			DisableErrSkip:       true,
			OmitConnResetSession: true,
			OmitRows:             true,
			SpanFilter:           hasParentSpan,
		}),
	}
	if provider != nil {
		options = append(options, otelsql.WithTracerProvider(provider))
	}
	return options
}

func hasParentSpan(ctx context.Context, _ otelsql.Method, _ string, _ []driver.NamedValue) bool {
	return trace.SpanContextFromContext(ctx).IsValid()
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"postgres",
	fx.Provide(NewConnection),
)
