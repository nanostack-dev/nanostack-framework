// Package config loads YAML configuration with ${VAR} interpolation for the fx
// modules.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// typed configuration: core/config and the Bind function that
// nano gen config writes.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
package config
