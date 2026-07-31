package apisec_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// spec covers the shapes an operation's `security` key can take: inheriting the
// document default, a single scheme, alternatives (OR), combined schemes (AND),
// an anonymous alternative, and an explicit opt-out.
const spec = `
openapi: 3.1.1
info: {title: apisec, version: "1.0.0"}
security:
  - Bearer: [global:read]
components:
  securitySchemes:
    Bearer: {type: http, scheme: bearer}
    ApiKey: {type: apiKey, in: header, name: X-API-Key}
    Signature: {type: apiKey, in: header, name: X-Signature}
paths:
  /inherits:
    get: {operationId: inherits, responses: {"204": {description: ok}}}
  /single:
    get:
      operationId: single
      security: [{Bearer: [flows:read]}]
      responses: {"204": {description: ok}}
  /either:
    get:
      operationId: either
      security: [{Bearer: [flows:read]}, {ApiKey: [flows:read]}]
      responses: {"204": {description: ok}}
  /either-different-scopes:
    get:
      operationId: eitherDifferent
      security: [{Bearer: [flows:write]}, {ApiKey: [flows:read]}]
      responses: {"204": {description: ok}}
  /both:
    get:
      operationId: both
      security: [{ApiKey: [flows:read], Signature: []}]
      responses: {"204": {description: ok}}
  /optional:
    get:
      operationId: optional
      security: [{Bearer: [flows:read]}, {}]
      responses: {"204": {description: ok}}
  /public:
    get:
      operationId: public
      security: []
      responses: {"204": {description: ok}}
`

func resolver(t *testing.T) *apisec.Resolver {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	r, err := apisec.NewResolver(doc)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	return r
}

func requirementsFor(t *testing.T, rr *apisec.Resolver, path string) apisec.Requirements {
	t.Helper()
	reqs, ok := rr.For(httptest.NewRequest(http.MethodGet, path, nil))
	if !ok {
		t.Fatalf("no route matched %s", path)
	}
	return reqs
}

func TestResolveShapes(t *testing.T) {
	rr := resolver(t)

	tests := []struct {
		path    string
		alts    int
		schemes []string
		public  bool
	}{
		{"/inherits", 1, []string{"Bearer"}, false},
		{"/single", 1, []string{"Bearer"}, false},
		{"/either", 2, []string{"ApiKey", "Bearer"}, false},
		{"/both", 1, []string{"ApiKey", "Signature"}, false},
		{"/optional", 2, []string{"Bearer"}, true},
		{"/public", 0, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			reqs := requirementsFor(t, rr, tc.path)
			if len(reqs) != tc.alts {
				t.Errorf("alternatives = %d, want %d", len(reqs), tc.alts)
			}
			if got := reqs.Schemes(); !equal(got, tc.schemes) {
				t.Errorf("schemes = %v, want %v", got, tc.schemes)
			}
			if got := reqs.Public(); got != tc.public {
				t.Errorf("public = %v, want %v", got, tc.public)
			}
		})
	}
}

// TestInheritsDocumentDefault pins the distinction between an absent `security`
// key (inherit) and an empty one (public) — conflating them would silently
// expose or lock down operations.
func TestInheritsDocumentDefault(t *testing.T) {
	rr := resolver(t)

	inherited := requirementsFor(t, rr, "/inherits")
	scopes, ok := inherited.ScopesFor("Bearer")
	if !ok || !equal(scopes, []string{"global:read"}) {
		t.Fatalf("inherited scopes = %v (found=%v), want [global:read]", scopes, ok)
	}

	if got := requirementsFor(t, rr, "/public"); len(got) != 0 || !got.Public() {
		t.Fatalf("explicit `security: []` = %#v, want no requirements", got)
	}
}

// TestEvaluateOr is the case the generated context-key mechanism could not
// express: two schemes, either sufficient on its own.
func TestEvaluateOr(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/either")

	t.Run("second alternative rescues the first", func(t *testing.T) {
		var tried []string
		err := reqs.Evaluate(
			context.Background(),
			req(),
			func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) error {
				tried = append(tried, s.Name)
				if s.Name == "Bearer" {
					return errors.New("no bearer token")
				}
				return nil
			},
		)
		if err != nil {
			t.Fatalf("Evaluate = %v, want nil", err)
		}
		if !equal(tried, []string{"Bearer", "ApiKey"}) {
			t.Errorf("tried %v, want document order [Bearer ApiKey]", tried)
		}
	})

	t.Run("first alternative short-circuits", func(t *testing.T) {
		var tried []string
		err := reqs.Evaluate(
			context.Background(),
			req(),
			func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) error {
				tried = append(tried, s.Name)
				return nil
			},
		)
		if err != nil {
			t.Fatalf("Evaluate = %v, want nil", err)
		}
		if !equal(tried, []string{"Bearer"}) {
			t.Errorf("tried %v, want only [Bearer]", tried)
		}
	})

	t.Run("all alternatives failing is unauthorized", func(t *testing.T) {
		err := reqs.Evaluate(
			context.Background(),
			req(),
			func(context.Context, *http.Request, apisec.SchemeRequirement) error {
				return errors.New("nope")
			},
		)
		if !errors.Is(err, apisec.ErrUnauthorized) {
			t.Fatalf("Evaluate = %v, want ErrUnauthorized", err)
		}
	})
}

// TestEvaluateScopesArePerAlternative guards the other thing flattening lost:
// the same scheme demanding different scopes in different alternatives.
func TestEvaluateScopesArePerAlternative(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/either-different-scopes")

	seen := map[string][]string{}
	reject := func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) error {
		seen[s.Name] = s.Scopes
		return errors.New("reject everything so both alternatives are visited")
	}
	err := reqs.Evaluate(context.Background(), req(), reject)
	if !errors.Is(err, apisec.ErrUnauthorized) {
		t.Fatalf("Evaluate = %v, want ErrUnauthorized", err)
	}
	if !equal(seen["Bearer"], []string{"flows:write"}) {
		t.Errorf("Bearer scopes = %v, want [flows:write]", seen["Bearer"])
	}
	if !equal(seen["ApiKey"], []string{"flows:read"}) {
		t.Errorf("ApiKey scopes = %v, want [flows:read]", seen["ApiKey"])
	}
}

// TestEvaluateAnd requires every scheme in one alternative to pass.
func TestEvaluateAnd(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/both")

	t.Run("one scheme is not enough", func(t *testing.T) {
		err := reqs.Evaluate(
			context.Background(),
			req(),
			func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) error {
				if s.Name == "Signature" {
					return errors.New("missing signature")
				}
				return nil
			},
		)
		if !errors.Is(err, apisec.ErrUnauthorized) {
			t.Fatalf("Evaluate = %v, want ErrUnauthorized when one of two schemes fails", err)
		}
	})

	t.Run("both schemes pass", func(t *testing.T) {
		var count int
		err := reqs.Evaluate(
			context.Background(),
			req(),
			func(context.Context, *http.Request, apisec.SchemeRequirement) error {
				count++
				return nil
			},
		)
		if err != nil {
			t.Fatalf("Evaluate = %v, want nil", err)
		}
		if count != 2 {
			t.Errorf("authenticator called %d times, want 2", count)
		}
	})
}

// TestEvaluateAnonymous checks that an anonymous alternative admits the request
// without the authenticator ever being consulted.
func TestEvaluateAnonymous(t *testing.T) {
	for _, path := range []string{"/optional", "/public"} {
		t.Run(path, func(t *testing.T) {
			reqs := requirementsFor(t, resolver(t), path)
			called := false
			err := reqs.Evaluate(
				context.Background(),
				req(),
				func(context.Context, *http.Request, apisec.SchemeRequirement) error {
					called = true
					return errors.New("should not be reached")
				},
			)
			if err != nil {
				t.Fatalf("Evaluate = %v, want nil", err)
			}
			if called && path == "/public" {
				t.Error("authenticator consulted for an operation with no security")
			}
		})
	}
}

// TestUnknownRouteIsNotADecision documents that an unmatched request yields
// ok=false rather than an empty (and therefore public) requirement set.
func TestUnknownRouteIsNotADecision(t *testing.T) {
	rr := resolver(t)
	reqs, ok := rr.For(httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if ok {
		t.Fatalf("For(/does-not-exist) ok = true, want false (got %#v)", reqs)
	}
	if reqs != nil {
		t.Errorf("requirements = %#v, want nil", reqs)
	}
}

func TestNewResolverRejectsNilDocument(t *testing.T) {
	if _, err := apisec.NewResolver(nil); err == nil {
		t.Fatal("NewResolver(nil) = nil error, want failure")
	}
}

func req() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/either", nil)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
