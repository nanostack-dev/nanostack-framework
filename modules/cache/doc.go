// Package cache provides the shared cache store and the typed Cache[T] to an
// fx application.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// redis/nanocache, which has the same Store methods and a
// slog Cache[T].
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
//
// The package is frozen: it gets no fixes besides security fixes that a
// consumer reaches. Its keys join the parts without escaping and its
// EvictPrefix passes them into a Redis glob, so a part that holds ':', '\',
// '*', '?', '[' or ']' can collide with another key or evict another scope.
// Use redis/nanocache for free-text key parts.
//
// Deprecated: removed with fx (ADR 0013). Use redis/nanocache; see
// docs/modules.md.
package cache
