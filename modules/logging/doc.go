// Package logging provides the zerolog logger and the process-wide log.Binder
// to an fx application.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// core/logging, which builds a slog logger from log.Settings.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
//
// Deprecated: removed with fx (ADR 0013). Use core/logging; see
// docs/modules.md.
package logging
