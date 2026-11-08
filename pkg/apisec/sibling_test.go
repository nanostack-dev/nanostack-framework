package apisec_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// A wrong method on a fixed path resolves to the parameter path beside it
// when that path has the method, with either matcher: chi and ServeMux both
// let /flows/{id} match /flows/search. kin-openapi's router, which the request
// validator uses and the v0.22.0 resolver used, matches nothing there. An
// authentication middleware that trusts the resolver alone then judges
// PUT /flows/search under the requirements of PUT /flows/{id}; RELEASING.md
// ("Upgrade from v0.22.0", step 6) tells a service to check the validator's
// router first.
func TestWrongMethodOnFixedPathResolvesToParameterSibling(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: t, version: "1"}
components:
  securitySchemes:
    Bearer: {type: http, scheme: bearer}
paths:
  /flows/search:
    post:
      security: []
      responses: {"200": {description: ok}}
  /flows/{id}:
    parameters: [{name: id, in: path, required: true, schema: {type: string}}]
    put:
      security: [{Bearer: []}]
      responses: {"204": {description: ok}}
`
	for name, opts := range map[string][]apisec.Option{"servemux": nil, "chi": {apisec.MatchChi()}} {
		rr, err := apisec.NewResolverFromDocument([]byte(doc), opts...)
		if err != nil {
			t.Fatal(err)
		}
		reqs, ok := rr.For(httptest.NewRequest(http.MethodPut, "/flows/search", nil))
		if !ok || !reqs.AllowsScheme("Bearer") {
			t.Errorf("%s: For(PUT /flows/search) = %v, %v; want the bearer requirement of PUT /flows/{id}",
				name, reqs, ok)
		}
		reqs, ok = rr.For(httptest.NewRequest(http.MethodPost, "/flows/search", nil))
		if !ok || !reqs.Public() {
			t.Errorf("%s: For(POST /flows/search) = %v, %v; want the public operation", name, reqs, ok)
		}
	}
}
