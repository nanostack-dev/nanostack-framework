package nanopg

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/lib/pq" // Registers the postgres driver.
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// pingTimeout bounds the connection check of NewDB, so an unreachable server
// fails the build quickly.
const pingTimeout = 5 * time.Second

// Module provides *DB, *sql.DB and transactor.Transactor.
var Module = nano.Module("postgres", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewDB, SQL, NewTransactor),
)

// Config is the connection and the pool. The yaml tags are the keys of the
// deprecated modules/postgres section, so one YAML file serves both.
type Config struct {
	// Host is the server host name or address.
	Host string `config:"host" json:"host" yaml:"host"`
	// Port is the server port.
	Port int `config:"port" default:"5432" validate:"min=1,max=65535" json:"port" yaml:"port"`
	// User is the role to connect as.
	User string `config:"user" json:"user" yaml:"user"`
	// Password is the password of User. It may stay empty for trust or
	// certificate authentication.
	Password secret.Secret `config:"password,optional" json:"password" yaml:"password"`
	// DB is the database name.
	DB string `config:"db" json:"db" yaml:"dbname"`
	// SSLMode is the lib/pq sslmode, such as disable or verify-full. Empty
	// keeps the driver default (require).
	SSLMode string `config:"sslmode,optional" json:"sslmode" yaml:"sslmode"`
	// MaxOpenConns limits the open connections of the pool.
	MaxOpenConns int `config:"max_open_conns" default:"25" validate:"min=1" json:"max_open_conns" yaml:"maxOpenConns"`
	// MaxIdleConns limits the idle connections of the pool.
	MaxIdleConns int `config:"max_idle_conns" default:"25" validate:"min=0" json:"max_idle_conns" yaml:"maxIdleConns"`
	// ConnMaxLifetime closes a connection after this age.
	ConnMaxLifetime time.Duration `config:"conn_max_lifetime" default:"5m" json:"conn_max_lifetime" yaml:"connMaxLifetime"`
}

// DSN returns the lib/pq connection string. Every value is quoted, so a
// password with spaces or quotes connects. Do not log it: it holds the
// password in clear.
func (c Config) DSN() string {
	parts := []string{
		"host=" + quote(c.Host),
		"port=" + strconv.Itoa(c.Port),
		"user=" + quote(c.User),
		"dbname=" + quote(c.DB),
	}
	if c.Password.IsSet() {
		parts = append(parts, "password="+quote(c.Password.Reveal()))
	}
	if c.SSLMode != "" {
		parts = append(parts, "sslmode="+quote(c.SSLMode))
	}
	return strings.Join(parts, " ")
}

// quote writes a lib/pq key/value value: single quotes, with backslash and
// quote escaped.
func quote(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}

// DB is the connection pool. It is a nano Stopper, which closes the pool, and
// a HealthChecker, which pings it.
type DB struct {
	sql    *sql.DB
	logger *slog.Logger
}

// NewDB opens the pool with the query tracing of modules/postgres and pings
// the server. A failed ping closes the pool and fails the build.
func NewDB(ctx context.Context, cfg Config, logger *slog.Logger, tp trace.TracerProvider) (*DB, error) {
	logger = logger.With(slog.String(log.KeyComponent, "postgres"))
	logger.InfoContext(ctx, "connecting to postgres",
		slog.String("host", cfg.Host), slog.Int("port", cfg.Port), slog.String("db", cfg.DB))
	db, err := otelsql.Open("postgres", cfg.DSN(), traceOptions(tp)...)
	if err != nil {
		return nil, fmt.Errorf("nanopg: open: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("nanopg: ping %s:%d: %w", cfg.Host, cfg.Port, err)
	}
	return &DB{sql: db, logger: logger}, nil
}

// Stop closes the pool. It waits for the queries in progress.
func (d *DB) Stop(ctx context.Context) error {
	d.logger.InfoContext(ctx, "closing postgres connection pool")
	return d.sql.Close()
}

// CheckHealth pings the server, for /readyz.
func (d *DB) CheckHealth(ctx context.Context) error {
	return d.sql.PingContext(ctx)
}

// SQL returns the pool.
func SQL(d *DB) *sql.DB { return d.sql }

// NewTransactor returns the transactor over db. It is the only way a service
// runs a statement (docs/decision-log.md, "Single Query Entry Point").
func NewTransactor(db *sql.DB) transactor.Transactor {
	return transactor.New(db)
}

// traceOptions makes queries child spans of the caller's trace. Span names are
// the database/sql method ("sql.conn.query"), the statement text is recorded
// without bind arguments, and pings, session resets and row iteration produce
// no spans. A query whose context carries no span, such as a migration, a
// poller or a lock heartbeat, starts no trace of its own.
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
