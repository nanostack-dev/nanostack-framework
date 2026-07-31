// Package apisec wires a security-requirement resolver into an FX application.
//
// The resolver itself is an FX-free primitive in pkg/apisec; this module only
// owns its construction from the application's embedded OpenAPI document, so
// every service does not repeat the same provider.
package apisec

import (
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
	"go.uber.org/fx"
)

// Document is the raw OpenAPI contract a service serves. Applications supply it
// with fx.Supply(apisec.Document(embeddedSpec)).
type Document []byte

// NewModule provides an *apisec.Resolver built from the supplied Document.
//
// Construction happens at startup, so a contract the resolver cannot index
// fails the application rather than silently leaving routes unguarded at
// request time.
func NewModule() fx.Option {
	return fx.Module(
		"apisec",
		fx.Provide(
			func(doc Document) (*apisec.Resolver, error) {
				return apisec.NewResolverFromDocument(doc)
			},
		),
	)
}
