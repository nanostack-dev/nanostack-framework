// Package nano is the runtime core of a Nanostack service: lifecycle
// interfaces, the App that starts and stops components in dependency order,
// the admin health handlers, Main, and the manifest declarations that
// nano gen wire reads.
//
// The package uses no reflection. A component opts into lifecycle by
// implementing Starter, Stopper, Runner or HealthChecker; the generated Build
// function detects that from the provider's return type and registers it.
//
// The App logs lifecycle events through log/slog under core/log's keys. Each
// start, Runner and stop failure gives one Error line, where it happens, with
// the component; the summary lines ("nano: stopped", "nano: exiting") are Info
// and only count failures. Lifecycle lines do not name the service: the
// core/log handler adds the service key from its Config.Service. With another
// handler, or without Config.Service, give App.SetLogger a logger that
// carries the service name, for example
// logger.With(log.KeyService, "billing").
package nano

import "context"

// Starter is implemented by a component that must do work before the
// service takes traffic, such as warming a cache or running migrations.
// Start runs once, in dependency order, under the App's start timeout.
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by a component that holds a resource, such as a
// connection pool. Stop runs once, in reverse dependency order, under the
// App's stop deadline. A component that is also a Starter is stopped only
// when its Start succeeded.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Runner is implemented by a long-running loop: an HTTP server, a queue
// worker, a scheduler. Run blocks until ctx is cancelled, then returns nil or
// ctx.Err(). A Runner that returns earlier, with or without an error, shuts
// the App down and makes App.Run fail.
type Runner interface {
	Run(ctx context.Context) error
}

// HealthChecker is implemented by a component that can report whether it is
// able to serve, such as a database pool. The App calls CheckHealth for
// /readyz. The method name is specific on purpose: a type with an unrelated
// Check method does not become a readiness probe.
type HealthChecker interface {
	CheckHealth(ctx context.Context) error
}
