package nanoapisec_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
)

const contract = `openapi: 3.0.3
info: {title: t, version: "1"}
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
security:
  - bearer: []
paths:
  /health:
    get:
      security: []
      responses: {"200": {description: ok}}
  /widgets:
    get:
      responses: {"200": {description: ok}}
`

func TestNewResolverIndexesTheDocument(t *testing.T) {
	t.Parallel()
	resolver, err := nanoapisec.NewResolver(nanoapisec.Document(contract))
	if err != nil {
		t.Fatal(err)
	}
	reqs, ok := resolver.For(httptest.NewRequest(http.MethodGet, "/widgets", nil))
	if !ok || !reqs.AllowsScheme("bearer") {
		t.Errorf("For(GET /widgets) = %v, %v; want the global bearer requirement", reqs, ok)
	}
	reqs, ok = resolver.For(httptest.NewRequest(http.MethodGet, "/health", nil))
	if !ok || !reqs.Public() {
		t.Errorf("For(GET /health) = %v, %v; want a public operation", reqs, ok)
	}
}

func TestNewResolverFailsOnADocumentThatDoesNotLoad(t *testing.T) {
	t.Parallel()
	if _, err := nanoapisec.NewResolver(nanoapisec.Document("openapi: [")); err == nil {
		t.Fatal("NewResolver accepted a malformed document")
	}
}
