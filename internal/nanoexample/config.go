package nanoexample

import (
	"net/http"

	"cirello.io/pglock"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
	"github.com/nanostack-dev/nanostack-framework/otel/nanotrace"
	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

type Config struct {
	App        nano.AppConfig     `config:"app"`
	Log        log.Settings       `config:"log"`
	Tracing    nanotrace.Config   `config:"tracing"`
	Postgres   nanopg.Config      `config:"postgres"`
	Migrations nanomigrate.Config `config:"migrations"`
	PGLock     nanolock.Config    `config:"pglock"`
	Redis      nanocache.Config   `config:"redis"`
	HTTP       nanoserver.Config  `config:"http"`
	Pprof      nanopprof.Config   `config:"pprof"`
}

// Service stands for the service's own code that takes the module values.
type Service struct{}

func NewService(_ transactor.Transactor, _ *pglock.Client, _ nanocache.Store, _ *apisec.Resolver) *Service {
	return &Service{}
}

// OpenAPIDocument stands for the service's embedded contract.
func OpenAPIDocument() nanoapisec.Document {
	return nanoapisec.Document(`openapi: 3.0.3
info: {title: nanoexample, version: "1"}
paths: {}
`)
}

func NewHandler(_ *Service) http.Handler { return http.NotFoundHandler() }
