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
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
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

// Authenticator verifies one security scheme against the request.
//
// A nil error means the scheme is satisfied with the scopes demanded, and the
// returned context is carried forward — so an authenticator can attach the
// principal it resolved, the tenant it belongs to, or anything else the
// handlers need. That is the difference from openapi3filter's AuthenticationFunc,
// which returns only an error and therefore cannot hand anything downstream.
//
// Any error means the scheme is not satisfied and Evaluate moves on to the next
// alternative. Return ErrSchemeNotAttempted when the credential this scheme
// reads is simply absent: Evaluate then treats the alternative as inapplicable
// rather than rejected, and reports a real rejection from another alternative in
// preference to it.
//
// Evaluate may call an Authenticator more than once for one request, once per
// alternative naming the scheme, so implementations must be cheap when the
// credential is absent — return ErrSchemeNotAttempted before calling any remote
// verifier.
type Authenticator func(ctx context.Context, r *http.Request, scheme SchemeRequirement) (context.Context, error)

// ErrUnauthorized is returned by Evaluate when no alternative is satisfied. It
// wraps the failure of each alternative that was tried, so a caller can pull its
// own error type back out with errors.As and render the response it wants.
var ErrUnauthorized = errors.New("no security requirement satisfied")

// ErrSchemeNotAttempted reports that the credential a scheme reads is not
// present on the request, so the scheme was never really tried. Evaluate
// prefers any other failure when deciding what to report, which keeps the
// rejection a caller surfaces tied to the credential the client actually sent.
var ErrSchemeNotAttempted = errors.New("credential not present for scheme")

// Evaluate applies the requirements to a request and returns the context the
// satisfied alternative produced.
//
// Alternatives are tried in the order the document declares them and the first
// one that holds wins, so a document listing its cheapest or most common scheme
// first keeps the common path short. A partially-applied alternative's context
// is discarded when that alternative fails, so a rejected scheme cannot leave
// anything behind for the next one.
//
// A nil error means the request is authorised and the returned context should
// replace the request's. Otherwise the original context is returned unchanged
// with an error wrapping ErrUnauthorized. Failures that are only
// ErrSchemeNotAttempted are reported last, so a caller inspecting the error sees
// the rejection belonging to the credential the client actually sent.
func (reqs Requirements) Evaluate(
	ctx context.Context, r *http.Request, auth Authenticator,
) (context.Context, error) {
	if len(reqs) == 0 {
		return ctx, nil
	}

	var attempted, absent []error
	for _, req := range reqs {
		if req.Anonymous() {
			return ctx, nil
		}
		authorized, err := satisfy(ctx, r, req, auth)
		if err == nil {
			return authorized, nil
		}
		if errors.Is(err, ErrSchemeNotAttempted) {
			absent = append(absent, err)
			continue
		}
		attempted = append(attempted, err)
	}
	return ctx, fmt.Errorf("%w: %w", ErrUnauthorized, errors.Join(append(attempted, absent...)...))
}

func satisfy(
	ctx context.Context, r *http.Request, req Requirement, auth Authenticator,
) (context.Context, error) {
	// An alternative is a map in OpenAPI, so it has no order: sorting makes a
	// failing multi-scheme alternative always report the same scheme.
	schemes := append([]SchemeRequirement(nil), req.Schemes...)
	sort.Slice(schemes, func(i, j int) bool { return schemes[i].Name < schemes[j].Name })

	return satisfyEach(ctx, ctx, r, schemes, auth)
}

func satisfyEach(
	base, authorized context.Context,
	r *http.Request,
	schemes []SchemeRequirement,
	auth Authenticator,
) (context.Context, error) {
	if len(schemes) == 0 {
		return authorized, nil // every scheme of the alternative passed
	}

	// Each scheme sees what the previous ones attached to the context.
	next, err := auth(authorized, r, schemes[0])
	if err != nil {
		// Hand back the original context: a rejected alternative leaves nothing behind.
		return base, fmt.Errorf("scheme %q: %w", schemes[0].Name, err)
	}
	if next == nil {
		next = authorized // the scheme passed without adding anything
	}
	return satisfyEach(base, next, r, schemes[1:], auth)
}

// Resolver maps an incoming request to the security requirements of the
// operation it addresses.
//
// It must pick the operation the router dispatches to, or a request can be
// authenticated against one operation and served by another. So the index is
// a private http.ServeMux with one pattern per operation, the pattern that
// oapi-codegen's std-http server registers ("GET /files/{name}"): path
// decoding, a GET pattern serving HEAD, and the precedence of a literal
// segment over a wildcard are those of nanohttp.NewRouter. The document's
// servers are ignored, as the router ignores them; a base path is the
// router's BaseURL, which the Resolver does not model.
//
// A chi router needs MatchChi, and a router that refuses HEAD on a GET
// pattern needs ExplicitHEAD.
type Resolver struct {
	mux     *http.ServeMux
	chi     *chi.Mux // with MatchChi, in place of mux
	byRoute map[string]Requirements
	opts    options
}

// errRouteIndex reports a route that the index cannot take.
var errRouteIndex = errors.New("route cannot be indexed")

// Option configures a Resolver to match the router that serves the
// document.
type Option func(*options)

type options struct {
	explicitHEAD bool
	chi          bool
}

// ExplicitHEAD makes a GET operation stop matching HEAD, as
// nanohttp.RouterOptions.ExplicitHEAD does: pass the router's value. A HEAD
// request then resolves only to a HEAD operation.
func ExplicitHEAD(on bool) Option {
	return func(o *options) { o.explicitHEAD = on }
}

// MatchChi indexes the operations in a chi router and matches a request as
// a chi router does: on the escaped path, so GET /files/%61dmin reaches
// /files/{name}, not /files/admin, and with chi's precedence between
// siblings. Use it when chi serves the document. The default matches as
// http.ServeMux and nanohttp.NewRouter do.
func MatchChi() Option {
	return func(o *options) { o.chi = true }
}

// NewResolverFromDocument builds a Resolver from a raw OpenAPI document, which
// is how applications with an embedded contract construct one. Loading here
// keeps every consumer from repeating the loader boilerplate, and a malformed
// document fails at construction rather than leaving routes unresolvable.
func NewResolverFromDocument(raw []byte, opts ...Option) (*Resolver, error) {
	doc, err := openapi3.NewLoader().LoadFromData(raw)
	if err != nil {
		return nil, fmt.Errorf("apisec: load OpenAPI document: %w", err)
	}
	return NewResolver(doc, opts...)
}

// NewResolver builds a Resolver over an already-loaded OpenAPI document. The
// route index is built once here, not per request. It fails on a path that a
// ServeMux pattern cannot express, such as a parameter that is only part of
// a segment (/files/{name}.json), and on two paths that match the same
// requests.
func NewResolver(spec *openapi3.T, opts ...Option) (*Resolver, error) {
	if spec == nil {
		return nil, errors.New("apisec: nil OpenAPI document")
	}
	rr := &Resolver{byRoute: map[string]Requirements{}}
	for _, opt := range opts {
		opt(&rr.opts)
	}
	if rr.opts.chi {
		rr.chi = chi.NewRouter()
	} else {
		rr.mux = http.NewServeMux()
	}
	global := convert(&spec.Security)
	var paths map[string]*openapi3.PathItem
	if spec.Paths != nil {
		paths = spec.Paths.Map()
	}
	for _, path := range sortedKeys(paths) {
		route := path
		if !rr.opts.chi {
			var err error
			if route, err = routePath(path); err != nil {
				return nil, err
			}
		}
		for method, op := range paths[path].Operations() {
			reqs := global
			// An operation-level `security` key overrides the document-level
			// default, including when it is present but empty, which is how a
			// document marks a single operation public. A nil key inherits.
			if op.Security != nil {
				reqs = convert(op.Security)
			}
			pattern := method + " " + route
			if err := rr.register(method, route); err != nil {
				return nil, fmt.Errorf("apisec: index %s %s: %w", method, path, err)
			}
			rr.byRoute[pattern] = reqs
		}
	}
	return rr, nil
}

// register adds the route of method to the index. ServeMux and chi panic on
// a pattern they cannot take, such as one that conflicts with another; that
// becomes an error.
func (rr *Resolver) register(method, route string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errRouteIndex, r)
		}
	}()
	if rr.chi != nil {
		rr.chi.Method(method, route, http.NotFoundHandler())
		return nil
	}
	rr.mux.Handle(method+" "+route, http.NotFoundHandler())
	return nil
}

// routePath turns an OpenAPI path into the path of a ServeMux pattern. Each
// parameter becomes a positional wildcard, since its value is not read and
// its name need not be a Go identifier.
func routePath(path string) (string, error) {
	segments := strings.Split(path, "/")
	n := 0
	for i, seg := range segments {
		if !strings.ContainsAny(seg, "{}") {
			continue
		}
		if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") || strings.Count(seg, "{") != 1 {
			return "", fmt.Errorf("apisec: path %s: a parameter must be a whole segment", path)
		}
		segments[i] = "{p" + strconv.Itoa(n) + "}"
		n++
	}
	return strings.Join(segments, "/"), nil
}

func sortedKeys(m map[string]*openapi3.PathItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// For returns the requirements governing r.
//
// ok is false when the request matches no operation of the document. On the
// nano stack, request validation runs inside the handler, after routing, so
// nothing rejects such a request before this point: refuse it (401 or 404),
// never let it through. The router answers it with 404 or 405 anyway, unless
// the router serves a route the document does not list.
func (rr *Resolver) For(r *http.Request) (Requirements, bool) {
	var pattern string
	if rr.chi != nil {
		// chi routes on RawPath when it is set, else on Path.
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		if route := rr.chi.Find(chi.NewRouteContext(), r.Method, path); route != "" {
			pattern = r.Method + " " + route
		}
	} else {
		_, pattern = rr.mux.Handler(r)
	}
	if pattern == "" {
		return nil, false
	}
	if rr.opts.explicitHEAD && r.Method == http.MethodHead && strings.HasPrefix(pattern, http.MethodGet+" ") {
		return nil, false
	}
	reqs, ok := rr.byRoute[pattern]
	return reqs, ok
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
