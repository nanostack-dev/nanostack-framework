// Package logging is the nano module that builds the process logger from
// log.Settings and installs it as the slog default.
//
// It replaces the deprecated fx module modules/logging. A service lists
// Module in its manifest and puts log.Settings in its Config, so the env names
// stay LOG_LEVEL, LOG_FORMAT, LOG_WIRE, ENVIRONMENT, SERVICE_NAME and
// SERVICE_VERSION:
//
//	type Config struct {
//		App nano.AppConfig `config:"app"`
//		Log log.Settings   `config:"log"`
//	}
//
//	var App = nano.Manifest("billing", logging.Module, nanotrace.Module, ...)
//
// Other modules add context attributes, such as trace ids, as members of
// nano.Collect[log.ContextAttrs]. Module declares the collection, so it can be
// empty.
//
// The package is a separate package from core/log because core/nano imports
// core/log: a module value in core/log would make an import cycle.
package logging
