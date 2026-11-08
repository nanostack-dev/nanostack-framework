// Package migrations applies golang-migrate migrations while an fx
// application is built.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// pg/nanomigrate, which runs as a nano.Invoke.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
package migrations
