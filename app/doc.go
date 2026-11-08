// Package app assembles an fx application from explicit fx options.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code declares a
// core/nano manifest, generates its wiring with `nano gen wire`, and runs it
// with nano.Main. See docs/modules.md for the migration.
//
// Deprecated: removed with fx (ADR 0013). Use a core/nano manifest with nano
// gen wire and nano.Main; see docs/modules.md.
package app
