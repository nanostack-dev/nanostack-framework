// Package nanoserver is the nano module that serves a service's HTTP handler:
// it listens when the App runs and drains in-flight requests when the App
// stops.
//
// It replaces the server part of the deprecated fx module
// modules/httpserver, and the goroutine, signal handler and Fatal call that
// services wrote around it. The service keeps its router, its middleware
// order and its /health route, and provides the result as an http.Handler:
//
//	type Config struct {
//		HTTP nanoserver.Config `config:"http"`
//	}
//
//	var App = nano.Manifest("billing",
//		nanoserver.Module,
//		nano.Providers(NewRouter), // func NewRouter(...) http.Handler
//	)
//
// The service adds tracing (pkg/tracing.Middleware) and request logging
// (core/log.HTTPMiddleware) in its own handler chain, in the order it wants.
package nanoserver
