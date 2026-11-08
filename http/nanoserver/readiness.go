package nanoserver

import (
	"context"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// ReadinessName is the key of the readiness result in a legacy /health body.
const ReadinessName = "readiness"

// Readiness reports the App's readiness to the legacy /health endpoint.
//
// /health stays a contract in the beta: it is the Docker HEALTHCHECK and gate
// flows assert it. Its body comes from pkg/health, which a layered package may
// not import. Readiness has the method set of pkg/health.Checker (Name and
// Check), so the service registers it in its own registry and keeps one
// implementation of the body:
//
//	registry := health.NewRegistry()
//	registry.Register(nanoserver.NewReadiness(app))
//	mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Registry: registry}))
//
// The body then has "readiness": "HEALTHY" while the App runs and every
// nano HealthChecker passes, and the endpoint answers 503 otherwise. That
// key replaces the per-dependency keys, such as "postgres", that the fx
// modules registered.
type Readiness struct {
	app *nano.App
}

// NewReadiness returns the readiness check of app.
func NewReadiness(app *nano.App) Readiness { return Readiness{app: app} }

// Name returns ReadinessName.
func (Readiness) Name() string { return ReadinessName }

// Check returns nil when the App runs and every health check passes, and the
// joined failures otherwise.
func (r Readiness) Check(ctx context.Context) error { return r.app.Ready(ctx) }
