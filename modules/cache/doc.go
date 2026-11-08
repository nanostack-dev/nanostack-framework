// Package cache provides the shared cache store and the typed Cache[T] to an
// fx application.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// redis/nanocache, which has the same Store methods and a
// slog Cache[T].
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
package cache
