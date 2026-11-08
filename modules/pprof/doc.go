// Package pprof serves net/http/pprof on a loopback listener in an fx
// application when ENABLE_PPROF is set.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// http/nanopprof.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
//
// Deprecated: removed with fx (ADR 0013). Use http/nanopprof; see
// docs/modules.md.
package pprof
