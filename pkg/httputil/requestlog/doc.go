// Package requestlog logs one zerolog line for each HTTP request and keeps a
// request-scoped zerolog logger on the context.
//
// This package is deprecated and is removed with zerolog. It keeps working
// through the beta. New code uses core/log.HTTPMiddleware;
// HTTPOptions.LegacySummary keeps the keys of the legacy request line. See
// docs/modules.md for the migration.
//
// Deprecated: removed with fx (ADR 0013). Use core/log.HTTPMiddleware; see
// docs/modules.md.
package requestlog
