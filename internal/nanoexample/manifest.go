//go:build nanogen

package nanoexample

import (
	"github.com/nanostack-dev/nanostack-framework/core/logging"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
	"github.com/nanostack-dev/nanostack-framework/otel/nanotrace"
	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
	"github.com/nanostack-dev/nanostack-framework/redis/nanocache"
)

var App = nano.Manifest("nanoexample",
	logging.Module,
	nanotrace.Module,
	nanopg.Module,
	nanomigrate.Module,
	nanolock.Module,
	nanocache.Module,
	nanoserver.Module,
	nanopprof.Module,
	nanoapisec.Module,
	nano.Providers(OpenAPIDocument, NewService, NewHandler),
)
