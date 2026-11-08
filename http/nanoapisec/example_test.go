package nanoapisec_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
)

// The service provides its embedded contract as a Document; the module
// builds the resolver from it.
func ExampleNewResolver() {
	doc := nanoapisec.Document(`openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /health:
    get:
      responses: {"200": {description: ok}}
`)
	resolver, err := nanoapisec.NewResolver(doc)
	if err != nil {
		fmt.Println(err)
		return
	}
	reqs, ok := resolver.For(httptest.NewRequest(http.MethodGet, "/health", nil))
	fmt.Println(ok, reqs.Public())
	// Output: true true
}
