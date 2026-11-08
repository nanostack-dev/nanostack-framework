package nanoapisec

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// Module provides *apisec.Resolver from the service's Document.
var Module = nano.Module("apisec", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewResolver),
)

// Document is the raw OpenAPI contract that the service serves. The service
// provides it, usually from an embedded file.
type Document []byte

// NewResolver indexes doc and returns the resolver of its security
// requirements, matched like nanohttp.NewRouter with its default options. It
// fails when doc does not load or cannot be indexed.
//
// A service whose router sets RouterOptions.ExplicitHEAD, or that routes
// with chi, provides its resolver with NewResolverWith and the options of
// its router instead of listing Module.
func NewResolver(doc Document) (*apisec.Resolver, error) {
	return apisec.NewResolverFromDocument(doc)
}

// NewResolverWith is NewResolver with options that match the router:
// apisec.ExplicitHEAD with the RouterOptions value, apisec.MatchChi for
// chi.
func NewResolverWith(doc Document, opts ...apisec.Option) (*apisec.Resolver, error) {
	return apisec.NewResolverFromDocument(doc, opts...)
}
