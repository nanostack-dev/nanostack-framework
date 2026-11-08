package nanoapisec_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nanostack-dev/nanostack-framework/http/nanoapisec"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// contract builds a document with a templated operation next to its literal
// sibling, and a protected operation that a percent-encoded path reaches.
// publicTemplate decides which of the two siblings is public: either way, a
// resolver that disagrees with the router authenticates a request against
// the wrong one.
func contractFor(publicTemplate bool) (string, map[string]bool) {
	template, literal := "security: []", ""
	if !publicTemplate {
		template, literal = "", "security: []"
	}
	doc := `openapi: 3.0.3
info: {title: t, version: "1"}
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
security:
  - bearer: []
paths:
  /files/{name}:
    get:
      ` + template + `
      parameters: [{name: name, in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok}}
  /files/admin:
    get:
      ` + literal + `
      responses: {"200": {description: ok}}
  /secret:
    get:
      responses: {"200": {description: ok}}
`
	return doc, map[string]bool{"/files/{name}": publicTemplate, "/files/admin": !publicTemplate, "/secret": false}
}

// verdict is what a request means for authentication: the operation it
// reaches and whether that operation is public.
type verdict struct {
	op     string // "" when no operation is reached
	public bool
}

var requests = []struct{ method, target string }{ //nolint:gochecknoglobals // test table.
	{http.MethodGet, "/files/%61dmin"},
	{http.MethodGet, "/%73ecret"},
	{http.MethodGet, "/files/admin"},
	{http.MethodGet, "/files/other"},
	{http.MethodGet, "/files/a%2Fb"},
	{http.MethodHead, "/secret"},
	{http.MethodHead, "/files/admin"},
	{http.MethodGet, "/missing"},
}

func muxFor(operations map[string]bool, ran *string) *http.ServeMux {
	mux := http.NewServeMux()
	for path := range operations {
		mux.HandleFunc("GET "+path, func(http.ResponseWriter, *http.Request) { *ran = path })
	}
	return mux
}

func dispatch(operations map[string]bool, h http.Handler, ran *string, r *http.Request) verdict {
	*ran = ""
	h.ServeHTTP(httptest.NewRecorder(), r)
	return verdict{op: *ran, public: operations[*ran]}
}

// resolved is what the resolver decides. An unresolved request is refused,
// so it can never reach a handler unauthenticated.
func resolved(rr *apisec.Resolver, r *http.Request) (public, ok bool) {
	reqs, ok := rr.For(r)
	return ok && reqs.Public(), ok
}

// agree checks every request: when the router runs a handler, the resolver
// must resolve the request, to an operation with the same publicity. An
// unresolved request is refused, so it cannot reach a handler.
func agree(
	t *testing.T,
	name string,
	operations map[string]bool,
	rr *apisec.Resolver,
	router http.Handler,
	ran *string,
) {
	t.Helper()
	for _, tc := range requests {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		got := dispatch(operations, router, ran, r)
		public, ok := resolved(rr, r)
		if got.op != "" && (!ok || public != got.public) {
			t.Errorf("%s: %s %s: router runs %s (public=%v), resolver says public=%v ok=%v",
				name, tc.method, tc.target, got.op, got.public, public, ok)
		}
	}
}

func TestResolverAgreesWithNewRouter(t *testing.T) {
	t.Parallel()
	for _, publicTemplate := range []bool{true, false} {
		doc, operations := contractFor(publicTemplate)
		for _, explicitHEAD := range []bool{false, true} {
			rr, err := nanoapisec.NewResolverWith(nanoapisec.Document(doc), apisec.ExplicitHEAD(explicitHEAD))
			if err != nil {
				t.Fatal(err)
			}
			var ran string
			router := nanohttp.NewRouter(muxFor(operations, &ran), nanohttp.RouterOptions{ExplicitHEAD: explicitHEAD})
			agree(t, fmt.Sprintf("publicTemplate=%v ExplicitHEAD=%v", publicTemplate, explicitHEAD),
				operations, rr, router, &ran)
		}
	}
}

// echopoint and anchor still route with chi, which matches the escaped path:
// MatchChi makes the resolver agree with it.
func TestChiResolverAgreesWithChi(t *testing.T) {
	t.Parallel()
	for _, publicTemplate := range []bool{true, false} {
		doc, operations := contractFor(publicTemplate)
		rr, err := nanoapisec.NewResolverWith(nanoapisec.Document(doc), apisec.MatchChi())
		if err != nil {
			t.Fatal(err)
		}
		var ran string
		router := chi.NewRouter()
		for path := range operations {
			router.Get(path, func(http.ResponseWriter, *http.Request) { ran = path })
		}
		agree(t, fmt.Sprintf("chi publicTemplate=%v", publicTemplate), operations, rr, router, &ran)
	}
}

// The default resolver matches the decoded path, so under chi it picks the
// literal sibling of /files/%61dmin while chi runs the template: a service
// that routes with chi must pass MatchChi.
func TestDecodedPathResolverDisagreesWithChi(t *testing.T) {
	t.Parallel()
	doc, _ := contractFor(false)
	rr, err := nanoapisec.NewResolver(nanoapisec.Document(doc))
	if err != nil {
		t.Fatal(err)
	}
	if public, ok := resolved(rr, httptest.NewRequest(http.MethodGet, "/files/%61dmin", nil)); !ok || !public {
		t.Fatalf("decoded resolver on /files/%%61dmin: public=%v ok=%v, want the public literal", public, ok)
	}
}

func TestNewResolverRejectsAPartialSegmentParameter(t *testing.T) {
	t.Parallel()
	doc := `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /files/{name}.json:
    get:
      parameters: [{name: name, in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok}}
`
	if _, err := nanoapisec.NewResolver(nanoapisec.Document(doc)); err == nil {
		t.Fatal("NewResolver accepted a parameter that is part of a segment")
	}
}
