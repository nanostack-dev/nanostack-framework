// Package httpserver runs the chi HTTP server of an fx application and maps
// errors of the strict oapi-codegen server to the legacy error envelope.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. New code uses
// http/nanoserver for the server; the service keeps its own
// router and middleware. StrictErrorHandler shares its resolution with
// pkg/httputil/errorenvelope, whose Writer logs through core/log.
// See docs/modules.md for the migration, one module at a time with
// modules/nanofx.
package httpserver
