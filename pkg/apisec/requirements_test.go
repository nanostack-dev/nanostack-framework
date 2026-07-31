package apisec_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		_, err := reqs.Evaluate(
			context.Background(),
			req(),
			func(ctx context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
				tried = append(tried, s.Name)
				if s.Name == "Bearer" {
					return nil, errors.New("no bearer token")
				}
				return ctx, nil
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
		_, err := reqs.Evaluate(
			context.Background(),
			req(),
			func(ctx context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
				tried = append(tried, s.Name)
				return ctx, nil
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
		_, err := reqs.Evaluate(
			context.Background(),
			req(),
			func(context.Context, *http.Request, apisec.SchemeRequirement) (context.Context, error) {
				return nil, errors.New("nope")
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
	reject := func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
		seen[s.Name] = s.Scopes
		return nil, errors.New("reject everything so both alternatives are visited")
	}
	_, err := reqs.Evaluate(context.Background(), req(), reject)
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
		_, err := reqs.Evaluate(
			context.Background(),
			req(),
			func(ctx context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
				if s.Name == "Signature" {
					return nil, errors.New("missing signature")
				}
				return ctx, nil
			},
		)
		if !errors.Is(err, apisec.ErrUnauthorized) {
			t.Fatalf("Evaluate = %v, want ErrUnauthorized when one of two schemes fails", err)
		}
	})

	t.Run("both schemes pass", func(t *testing.T) {
		var count int
		_, err := reqs.Evaluate(
			context.Background(),
			req(),
			func(ctx context.Context, _ *http.Request, _ apisec.SchemeRequirement) (context.Context, error) {
				count++
				return ctx, nil
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
			_, err := reqs.Evaluate(
				context.Background(),
				req(),
				func(context.Context, *http.Request, apisec.SchemeRequirement) (context.Context, error) {
					called = true
					return nil, errors.New("should not be reached")
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

type principalKey struct{}

// TestEvaluateCarriesContextForward covers what openapi3filter's
// AuthenticationFunc cannot do: hand the verified principal downstream.
func TestEvaluateCarriesContextForward(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/either")

	authorized, err := reqs.Evaluate(
		context.Background(),
		req(),
		func(ctx context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
			if s.Name == "Bearer" {
				return nil, errors.New("no bearer token")
			}
			return context.WithValue(ctx, principalKey{}, "user-42"), nil
		},
	)
	if err != nil {
		t.Fatalf("Evaluate = %v, want nil", err)
	}
	if got := authorized.Value(principalKey{}); got != "user-42" {
		t.Errorf("principal = %v, want user-42 carried out of the satisfied alternative", got)
	}
}

// TestEvaluateDiscardsFailedAlternativeContext ensures a rejected scheme cannot
// leave anything behind for the alternative tried after it.
func TestEvaluateDiscardsFailedAlternativeContext(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/both")

	_, err := reqs.Evaluate(
		context.Background(),
		req(),
		func(ctx context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
			if s.Name == "Signature" {
				return nil, errors.New("missing signature")
			}
			return context.WithValue(ctx, principalKey{}, "leaked"), nil
		},
	)
	if !errors.Is(err, apisec.ErrUnauthorized) {
		t.Fatalf("Evaluate = %v, want ErrUnauthorized", err)
	}
}

// TestEvaluateReportsAttemptedFailureFirst pins the error ordering a caller
// relies on to render the right rejection: the scheme whose credential the
// client actually sent, not the one that was never present.
func TestEvaluateReportsAttemptedFailureFirst(t *testing.T) {
	reqs := requirementsFor(t, resolver(t), "/either")

	badKey := errors.New("api key rejected")
	_, err := reqs.Evaluate(
		context.Background(),
		req(),
		func(_ context.Context, _ *http.Request, s apisec.SchemeRequirement) (context.Context, error) {
			if s.Name == "Bearer" {
				return nil, apisec.ErrSchemeNotAttempted
			}
			return nil, badKey
		},
	)
	if !errors.Is(err, apisec.ErrUnauthorized) {
		t.Fatalf("Evaluate = %v, want ErrUnauthorized", err)
	}
	if !errors.Is(err, badKey) {
		t.Errorf("error %v does not carry the real rejection", err)
	}

	joined := err.Error()
	realAt := strings.Index(joined, badKey.Error())
	absentAt := strings.Index(joined, apisec.ErrSchemeNotAttempted.Error())
	if realAt < 0 || absentAt < 0 || realAt > absentAt {
		t.Errorf("wanted the real rejection reported before the absent credential, got %q", joined)
	}
}
