// Package apisec wires a security-requirement resolver into an FX application.
//
// The resolver itself is an FX-free primitive in pkg/apisec; this module only
// owns its construction from the application's embedded OpenAPI document, so
// every service does not repeat the same provider.
//
// This package is deprecated and is removed with fx (ADR 0013). It keeps
// working, with its fx signatures, through the beta. It is a thin wrapper of
// the nano module http/nanoapisec, which new code lists in its manifest
// instead (see docs/modules.md).
//
// Deprecated: removed with fx (ADR 0013). Use http/nanoapisec; see
// docs/modules.md.
package apisec

import (
	"go.uber.org/fx"

	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// Document is the raw OpenAPI contract a service serves. Applications supply it
// with fx.Supply(apisec.Document(embeddedSpec)).
type Document []byte

// NewModule provides an *apisec.Resolver built from the supplied Document.
// It matches requests as chi does (apisec.MatchChi), since an fx service
// routes with chi. Its nano replacement is nanoapisec.ChiModule; a service
// that moves to nanohttp.NewRouter lists nanoapisec.Module instead.
//
// Construction happens at startup, so a contract the resolver cannot index
// fails the application rather than silently leaving routes unguarded at
// request time.
func NewModule() fx.Option {
	return fx.Module(
		"apisec",
		fx.Provide(
			func(doc Document) (*apisec.Resolver, error) {
				return nanoapisec.NewChiResolver(nanoapisec.Document(doc))
			},
		),
	)
}
