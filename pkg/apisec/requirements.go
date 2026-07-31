// Package apisec resolves and evaluates the OpenAPI `security` requirements
// that apply to an incoming request.
//
// oapi-codegen can emit the requirements into the request context, one
// `<Scheme>Scopes` context key per scheme, and middleware then reads those keys
// back. That mechanism flattens the requirements: it records which schemes are
// mentioned somewhere on the operation and the scopes each carries, but loses
// the structure joining them. It cannot express alternative schemes (OR),
// combined schemes (AND), or an anonymous `{}` alternative, so callers end up
// hand-rolling the dispatch. oapi-codegen deprecated it for that reason
// (oapi-codegen#1524) and now hides it behind
// `compatibility.enable-auth-scopes-on-context`.
//
// This package reads the requirements from the OpenAPI document instead, which
// keeps their structure intact, and evaluates them with the semantics the
// specification defines:
//
//   - the requirement list is a disjunction — the request is authorised as soon
//     as one entry is satisfied;
//   - the schemes within one entry are a conjunction — all of them must be
//     satisfied together;
//   - an empty entry `{}` is an anonymous alternative, so the operation may be
//     reached without credentials;
//   - an empty list means the operation declares no security at all.
//
// Authentication itself stays with the caller: Evaluate takes an Authenticator,
// so the application keeps ownership of credential verification, of the context
// it derives from a verified principal, and of its error responses.
package apisec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// SchemeRequirement is one security scheme together with the scopes the
// operation demands of it. Scopes is empty when the scheme is required but
// carries no scopes.
type SchemeRequirement struct {
	Name   string
	Scopes []string
}

// Requirement is a single alternative: every scheme it lists must be satisfied
// for the alternative to hold. A Requirement with no schemes is the anonymous
// alternative and is always satisfied.
type Requirement struct {
	Schemes []SchemeRequirement
}

// Anonymous reports whether this alternative admits an unauthenticated request.
func (req Requirement) Anonymous() bool { return len(req.Schemes) == 0 }

// Requirements is the set of alternatives attached to an operation. Satisfying
// any one of them authorises the request.
type Requirements []Requirement

// Public reports whether the operation declares no security requirements, or
// offers an anonymous alternative. Either way the request may proceed without
// credentials.
func (reqs Requirements) Public() bool {
	if len(reqs) == 0 {
		return true
	}
	for _, req := range reqs {
		if req.Anonymous() {
			return true
		}
	}
	return false
}

// AllowsScheme reports whether any alternative mentions the named scheme.
func (reqs Requirements) AllowsScheme(name string) bool {
	for _, req := range reqs {
		for _, scheme := range req.Schemes {
			if scheme.Name == name {
				return true
			}
		}
	}
	return false
}

// ScopesFor returns the scopes demanded of the named scheme, and whether the
// scheme appears at all. When several alternatives name the same scheme with
// different scopes, the first occurrence wins; prefer Evaluate, which considers
// each alternative on its own terms.
func (reqs Requirements) ScopesFor(name string) ([]string, bool) {
	for _, req := range reqs {
		for _, scheme := range req.Schemes {
			if scheme.Name == name {
				return scheme.Scopes, true
			}
		}
	}
	return nil, false
}

// Schemes lists every scheme named across all alternatives, sorted, without
// duplicates.
func (reqs Requirements) Schemes() []string {
	seen := map[string]bool{}
	var out []string
	for _, req := range reqs {
		for _, scheme := range req.Schemes {
			if !seen[scheme.Name] {
				seen[scheme.Name] = true
				out = append(out, scheme.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Authenticator verifies one security scheme against the request. Returning nil
// means the scheme is satisfied with the scopes demanded; any error means it is
// not, and Evaluate moves on to the next alternative.
//
// Evaluate may call an Authenticator more than once for a single request, once
// per alternative naming the scheme, so implementations should be cheap when the
// credential the scheme reads is absent — return early rather than calling out
// to a remote verifier.
type Authenticator func(ctx context.Context, r *http.Request, scheme SchemeRequirement) error

// ErrUnauthorized is returned by Evaluate when no alternative is satisfied. It
// wraps the failure of each alternative that was tried.
var ErrUnauthorized = errors.New("no security requirement satisfied")

// Evaluate applies the requirements to a request.
//
// Alternatives are tried in the order the document declares them and the first
// one that holds wins, so a document listing its cheapest or most common scheme
// first keeps the common path short. A nil error means the request is
// authorised; otherwise the error wraps ErrUnauthorized and reports why each
// alternative failed.
func (reqs Requirements) Evaluate(ctx context.Context, r *http.Request, auth Authenticator) error {
	if len(reqs) == 0 {
		return nil
	}

	failures := make([]error, 0, len(reqs))
	for _, req := range reqs {
		if req.Anonymous() {
			return nil
		}
		if err := satisfy(ctx, r, req, auth); err != nil {
			failures = append(failures, err)
			continue
		}
		return nil
	}
	return fmt.Errorf("%w: %w", ErrUnauthorized, errors.Join(failures...))
}

// satisfy requires every scheme in the alternative to pass. Schemes are visited
// in sorted order so a multi-scheme alternative fails deterministically.
func satisfy(ctx context.Context, r *http.Request, req Requirement, auth Authenticator) error {
	schemes := append([]SchemeRequirement(nil), req.Schemes...)
	sort.Slice(schemes, func(i, j int) bool { return schemes[i].Name < schemes[j].Name })
	for _, scheme := range schemes {
		if err := auth(ctx, r, scheme); err != nil {
			return fmt.Errorf("scheme %q: %w", scheme.Name, err)
		}
	}
	return nil
}

// Resolver maps an incoming request to the security requirements of the
// operation it addresses.
type Resolver struct {
	router routers.Router
	global Requirements
}

// NewResolver builds a Resolver over an already-loaded OpenAPI document. The
// route index is built once here, not per request.
func NewResolver(spec *openapi3.T) (*Resolver, error) {
	if spec == nil {
		return nil, errors.New("apisec: nil OpenAPI document")
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, fmt.Errorf("apisec: index routes: %w", err)
	}
	return &Resolver{router: router, global: convert(&spec.Security)}, nil
}

// For returns the requirements governing r.
//
// ok is false when the request matches no operation in the document. That is
// not an authorisation decision: the caller decides what an unknown route
// means. Requests that never reach a documented operation are normally rejected
// by request validation before reaching this point.
func (rr *Resolver) For(r *http.Request) (Requirements, bool) {
	route, _, err := rr.router.FindRoute(r)
	if err != nil || route == nil || route.Operation == nil {
		return nil, false
	}
	// An operation-level `security` key overrides the document-level default,
	// including when it is present but empty — which is how a document marks a
	// single operation public. A nil key means "inherit".
	if route.Operation.Security == nil {
		return rr.global, true
	}
	return convert(route.Operation.Security), true
}

// convert turns kin-openapi's map-based requirements into the ordered form this
// package exposes. Scheme names are sorted because the underlying type is a
// map, whose iteration order would otherwise vary between runs.
func convert(src *openapi3.SecurityRequirements) Requirements {
	if src == nil || len(*src) == 0 {
		return nil
	}
	out := make(Requirements, 0, len(*src))
	for _, raw := range *src {
		names := make([]string, 0, len(raw))
		for name := range raw {
			names = append(names, name)
		}
		sort.Strings(names)

		schemes := make([]SchemeRequirement, 0, len(names))
		for _, name := range names {
			scopes := raw[name]
			if scopes == nil {
				scopes = []string{}
			}
			schemes = append(schemes, SchemeRequirement{Name: name, Scopes: scopes})
		}
		out = append(out, Requirement{Schemes: schemes})
	}
	return out
}
