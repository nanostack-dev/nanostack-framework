package nanoapisec

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// Module provides *apisec.Resolver from the service's Document, matched as
// http.ServeMux and nanohttp.NewRouter match. A service that routes with chi
// lists ChiModule instead.
var Module = nano.Module("apisec", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewResolver),
)

// ChiModule provides *apisec.Resolver from the service's Document, matched as
// a chi router matches. It is the drop-in replacement of the fx module
// modules/apisec, which matched as chi: a service that routes with chi lists
// ChiModule, and a service that routes with nanohttp.NewRouter lists Module.
var ChiModule = nano.Module("apisec-chi", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewChiResolver),
)

// Document is the raw OpenAPI contract that the service serves. The service
// provides it, usually from an embedded file.
type Document []byte

// NewResolver indexes doc and returns the resolver of its security
// requirements, matched like nanohttp.NewRouter with its default options. It
// fails when doc does not load or cannot be indexed.
//
// A service that routes with chi lists ChiModule instead of Module. A
// service whose router sets RouterOptions.ExplicitHEAD provides its resolver
// with NewResolverWith and the options of its router.
func NewResolver(doc Document) (*apisec.Resolver, error) {
	return apisec.NewResolverFromDocument(doc)
}

// NewChiResolver is NewResolver matched as a chi router matches
// (apisec.MatchChi): on the escaped path, with chi's precedence between
// siblings. ChiModule provides it.
func NewChiResolver(doc Document) (*apisec.Resolver, error) {
	return NewResolverWith(doc, apisec.MatchChi())
}

// NewResolverWith is NewResolver with options that match the router:
// apisec.ExplicitHEAD with the RouterOptions value, apisec.MatchChi for
// chi.
func NewResolverWith(doc Document, opts ...apisec.Option) (*apisec.Resolver, error) {
	return apisec.NewResolverFromDocument(doc, opts...)
}
