package gendb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq" // Registers the postgres driver.
	"github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

const (
	startTimeout = 2 * time.Minute
	dropTimeout  = 30 * time.Second
	// containerPassword is the superuser password of the container nano gen
	// starts; it listens on a random local port and lives for one run.
	containerPassword = "nanogen"
)

// Server is the PostgreSQL server of a run: the server that a
// NANO_GEN_DATABASE_URL names, or a container that it starts on the first
// Open. Each Open creates an empty scratch database; its drop function
// removes it. Close stops the container. The pipeline owns one Server for
// the whole run.
type Server struct {
	url, image string
	// run starts the container; tests replace it.
	run       func(ctx context.Context, image string) (testcontainers.Container, error)
	once      sync.Once
	adminDSN  string
	container testcontainers.Container
	err       error
}

// NewServer returns a Server for databaseURL, or, when it is empty, for a
// container of image. It starts nothing.
func NewServer(databaseURL, image string) *Server {
	return &Server{url: databaseURL, image: orDefault(image, DefaultImage), run: startContainer}
}

// Open returns an empty database and the function that drops it. It starts
// the server on its first call. Every error wraps gen.ErrEnvironment and
// carries the fix.
func (s *Server) Open(ctx context.Context) (*sql.DB, func(), error) {
	s.once.Do(func() { s.err = s.start(ctx) })
	if s.err != nil {
		return nil, nil, s.err
	}
	admin, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		return nil, nil, s.fail("open the server: %v", err)
	}
	name := "nanogen_" + strings.ToLower(rand.Text()[:12])
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close()
		return nil, nil, s.fail("create a scratch database: %v", err)
	}
	dsn, err := withDatabase(s.adminDSN, name)
	if err != nil {
		_ = admin.Close()
		return nil, nil, s.fail("%v", err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = admin.Close()
		return nil, nil, s.fail("open the scratch database: %v", err)
	}
	drop := func() {
		_ = db.Close()
		dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dropTimeout)
		defer cancel()
		_, _ = admin.ExecContext(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	}
	return db, drop, nil
}

// Close stops the container, if Open started one.
func (s *Server) Close() {
	if s.container != nil {
		_ = testcontainers.TerminateContainer(s.container)
		s.container = nil
	}
}

func (s *Server) start(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if s.url != "" {
		u, err := url.Parse(s.url)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			return &gen.Error{Msg: EnvDatabaseURL + " is not a postgres:// URL",
				Fix: "set " + EnvDatabaseURL + " to a URL such as postgres://user:password@localhost:5432/postgres, " +
					"or unset it to start a container",
				Err: gen.ErrEnvironment}
		}
		s.adminDSN = s.url
		return s.ping(ctx, "the server that "+EnvDatabaseURL+" names ("+u.Redacted()+") does not answer")
	}
	container, err := recovered(func() (testcontainers.Container, error) { return s.run(ctx, s.image) })
	if err != nil {
		return &gen.Error{
			Msg: fmt.Sprintf("no Docker daemon is reachable to start %s: %v", s.image, err),
			Fix: "start Docker, or set " + EnvDatabaseURL,
			Err: gen.ErrEnvironment,
		}
	}
	s.container = container
	endpoint, err := container.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		return s.fail("read the address of %s: %v", s.image, err)
	}
	s.adminDSN = (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("postgres", containerPassword),
		Host:     endpoint,
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}).String()
	return s.ping(ctx, s.image+" started but does not answer")
}

// startContainer starts image with testcontainers. Ryuk removes it if the
// process dies before Close. The entrypoint starts the server twice, once
// to initialise it and once for real: the second ready line is the one to
// wait for.
func startContainer(ctx context.Context, image string) (testcontainers.Container, error) {
	// A clean run prints nothing; testcontainers logs each step to stderr
	// through its global logger unless told otherwise.
	quietTestcontainers()
	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Image:        image,
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_PASSWORD": containerPassword},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(startTimeout),
		Logger:  tclog.NewNoopLogger(),
		Started: true,
	})
}

// recovered calls start. testcontainers panics instead of failing on some
// Docker hosts it cannot reach; that is the same environment failure.
func recovered(start func() (testcontainers.Container, error)) (container testcontainers.Container, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return start()
}

// quietTestcontainers silences testcontainers' global logger, once per
// process.
var quietTestcontainers = sync.OnceFunc(func() { //nolint:gochecknoglobals // a process-wide setting, set once
	tclog.SetDefault(tclog.NewNoopLogger())
})

func (s *Server) ping(ctx context.Context, what string) error {
	db, err := sql.Open("postgres", s.adminDSN)
	if err == nil {
		err = db.PingContext(ctx)
		_ = db.Close()
	}
	if err != nil {
		return s.fail("%s: %v", what, err)
	}
	return nil
}

// fail returns an environment error with the fix for this server.
func (s *Server) fail(format string, args ...any) *gen.Error {
	fix := "check that Docker runs, or set " + EnvDatabaseURL
	if s.url != "" {
		fix = "start the server that " + EnvDatabaseURL + " names, with a user that may create databases, " +
			"or unset it to start a container"
	}
	return &gen.Error{Msg: fmt.Sprintf(format, args...), Fix: fix, Err: gen.ErrEnvironment}
}

// withDatabase returns dsn with its database replaced by name.
func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse the server URL: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}
