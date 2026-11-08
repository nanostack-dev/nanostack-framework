package nanohttp

import (
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// RouterOptions configures NewRouter.
type RouterOptions struct {
	// NotFound answers a request whose path matches no pattern. Nil means a
	// 404 problem (problem.NotFound) through problem.Write.
	NotFound http.Handler
	// MethodNotAllowed answers a request whose path matches a pattern only
	// for other methods. The Allow header is already set when it runs. Nil
	// means a 405 problem (problem.MethodNotAllowed) through problem.Write.
	MethodNotAllowed http.Handler
	// ExplicitHEAD makes a GET pattern stop matching HEAD, as chi does: a
	// HEAD request on a path without a HEAD pattern goes to MethodNotAllowed,
	// and HEAD is not listed in Allow for a GET pattern. A pattern without a
	// method still matches HEAD.
	ExplicitHEAD bool
}

// probeMethods are the methods NewRouter tries to build the Allow header:
// the methods of RFC 9110 and PATCH. A pattern registered for any other
// method is not listed in Allow.
var probeMethods = []string{ //nolint:gochecknoglobals // read-only table.
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
}

// NewRouter returns mux with configurable answers for the requests no
// pattern matches. ServeMux answers those itself with a text/plain 404 or
// 405; a service that must keep its own error shape on them, such as a
// legacy envelope, passes NotFound and MethodNotAllowed handlers here.
//
// A request with an unclean path, such as //health or /a/../health, goes to
// mux.ServeHTTP for every method, which redirects it to the clean path (307
// on Go 1.27). The clean path then gets the answers of NewRouter. chi answers
// such a path with 404 at once.
//
// For each other request, NewRouter looks the request up with mux.Handler. A
// non-empty pattern is a match: the request goes to mux.ServeHTTP, which
// sets r.Pattern and the path values, and which also serves ServeMux's
// trailing-slash redirect. An empty pattern is a miss: NewRouter looks the
// path up again with each method of probeMethods, sets Allow to the methods
// that match (sorted, as ServeMux writes it), and calls MethodNotAllowed
// when there is one and NotFound otherwise.
//
// A matched request is looked up twice, once here and once in
// mux.ServeHTTP. A miss costs one lookup per probed method.
//
// So every status and redirect is the one of a bare ServeMux, with one
// difference: with ExplicitHEAD, a HEAD request on a GET pattern is a miss
// (405). Only the bodies of the 404 and 405 answers are NewRouter's.
//
// The design relies on ServeMux.Handler returning an empty pattern for a
// method mismatch and for no match, and on ServeMux cleaning a path as
// cleanPath does. The tests of this package pin that behaviour, so a Go
// release that changes it fails them.
func NewRouter(mux *http.ServeMux, opts RouterOptions) http.Handler {
	rt := &router{
		mux:              mux,
		notFound:         opts.NotFound,
		methodNotAllowed: opts.MethodNotAllowed,
		explicitHEAD:     opts.ExplicitHEAD,
	}
	if rt.notFound == nil {
		rt.notFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			problem.Write(w, r, problem.NotFound())
		})
	}
	if rt.methodNotAllowed == nil {
		rt.methodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			problem.Write(w, r, problem.MethodNotAllowed())
		})
	}
	return rt
}

type router struct {
	mux              *http.ServeMux
	notFound         http.Handler
	methodNotAllowed http.Handler
	explicitHEAD     bool
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ServeMux answers the asterisk-form request (OPTIONS *) with a 400 of
	// its own before any lookup.
	if r.RequestURI == "*" {
		rt.mux.ServeHTTP(w, r)
		return
	}
	// ServeMux redirects an unclean path before it matches a method. A probe
	// of that path with another method would find the redirect's pattern and
	// count the method as allowed, so the request goes to the mux unprobed.
	// ServeMux does not clean the path of a CONNECT request.
	if p := r.URL.EscapedPath(); r.Method != http.MethodConnect && cleanPath(p) != p {
		rt.mux.ServeHTTP(w, r)
		return
	}
	if _, pattern := rt.mux.Handler(r); pattern != "" && !rt.headOnGet(r.Method, pattern) {
		rt.mux.ServeHTTP(w, r)
		return
	}
	allow := rt.allowed(r)
	if len(allow) == 0 {
		rt.notFound.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Allow", strings.Join(allow, ", "))
	rt.methodNotAllowed.ServeHTTP(w, r)
}

// headOnGet reports whether a request of method that matched pattern is a
// HEAD request on a GET pattern, which ExplicitHEAD turns into a miss.
func (rt *router) headOnGet(method, pattern string) bool {
	return rt.explicitHEAD && method == http.MethodHead && strings.HasPrefix(pattern, http.MethodGet+" ")
}

// allowed returns the methods of probeMethods for which the path of r
// matches a pattern, sorted.
func (rt *router) allowed(r *http.Request) []string {
	probe := *r // ServeMux.Handler reads only Method, URL and Host
	var allow []string
	for _, method := range probeMethods {
		if method == r.Method {
			continue
		}
		probe.Method = method
		if _, pattern := rt.mux.Handler(&probe); pattern != "" && !rt.headOnGet(method, pattern) {
			allow = append(allow, method)
		}
	}
	slices.Sort(allow)
	return allow
}

// cleanPath returns the canonical path for p, as ServeMux computes it before
// it matches (net/http cleanPath): a leading slash, no . or .. element, no
// double slash, and the trailing slash of p kept.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}
