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
// requirements. It fails when doc does not load or cannot be indexed.
func NewResolver(doc Document) (*apisec.Resolver, error) {
	return apisec.NewResolverFromDocument(doc)
}
