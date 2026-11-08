// Package transactor provides the pkg/db/transactor Transactor to an fx
// application.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// pg/nanopg, which provides the same Transactor.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
package transactor
