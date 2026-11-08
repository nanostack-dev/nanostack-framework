package nanohttp

import (
	"net/http"
	"net/url"
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
// NewRouter registers a catch-all pattern "/" on mux. ServeMux tries the
// patterns of the request's method first, then GET for HEAD, then the
// patterns without a method, so the catch-all runs only on a real miss. Every
// request goes straight to mux.ServeHTTP, which sets r.Pattern and the path
// values, and keeps ServeMux's redirects: an unclean path, such as //health
// or /a/../health, is redirected to the clean path (307 on Go 1.27) for every
// method, and /tree to /tree/ when only the latter is a pattern. chi answers
// an unclean path with 404 at once.
//
// On a miss, the catch-all looks the path up with each method of
// probeMethods, sets Allow to the methods that match (sorted, as ServeMux
// writes it), and calls MethodNotAllowed when there is one and NotFound
// otherwise. It clears r.Pattern, which ServeMux set to the catch-all, so a
// 404 or 405 is logged without a route.
//
// So every status and redirect is the one of a bare ServeMux, with one
// difference: with ExplicitHEAD, a HEAD request on a GET pattern is a miss
// (405). Only the bodies of the 404 and 405 answers are NewRouter's.
//
// mux must not have a pattern without a method that matches every path ("/"
// or "/{path...}"): such a mux never misses, and NewRouter returns it
// unchanged. Register every pattern before calling NewRouter; a later "/"
// panics. Wrap a mux once: NewRouter panics on a mux it already wrapped,
// whose misses would otherwise reach the first router's handlers. The tests of this package pin the ServeMux behaviour the design
// relies on, so a Go release that changes it fails them.
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
	if !rt.registerCatchAll() {
		return mux
	}
	return rt
}

// catchAll is the pattern NewRouter registers for the misses.
const catchAll = "/"

// registerCatchAll registers the miss handler. It reports false when mux
// already has a pattern that conflicts with it, which matches every path.
func (rt *router) registerCatchAll() bool {
	if rt.tryRegister() {
		return true
	}
	probe := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: catchAll}}
	if h, _ := rt.mux.Handler(probe); isMissHandler(h) {
		panic("nanohttp: NewRouter: the mux is already wrapped by NewRouter; wrap a mux once")
	}
	return false
}

func (rt *router) tryRegister() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	rt.mux.Handle(catchAll, missHandler{rt})
	return true
}

// missHandler is the handler of the catch-all pattern.
type missHandler struct{ rt *router }

func (m missHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.rt.miss(w, r) }

func isMissHandler(h http.Handler) bool {
	_, ok := h.(missHandler)
	return ok
}

type router struct {
	mux              *http.ServeMux
	notFound         http.Handler
	methodNotAllowed http.Handler
	explicitHEAD     bool
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ServeMux serves HEAD with a GET pattern before it reaches the
	// catch-all, so ExplicitHEAD needs a lookup first. An unclean path keeps
	// its redirect.
	if rt.explicitHEAD && r.Method == http.MethodHead {
		if p := r.URL.EscapedPath(); cleanPath(p) == p {
			if _, pattern := rt.mux.Handler(r); rt.headOnGet(r.Method, pattern) {
				rt.answerMiss(w, r)
				return
			}
		}
	}
	rt.mux.ServeHTTP(w, r)
}

// miss is the handler of the catch-all pattern.
func (rt *router) miss(w http.ResponseWriter, r *http.Request) {
	// ServeMux set r.Pattern to the catch-all; a miss has no route.
	r.Pattern = ""
	rt.answerMiss(w, r)
}

func (rt *router) answerMiss(w http.ResponseWriter, r *http.Request) {
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
// matches a pattern other than the catch-all, sorted. Every lookup matches
// at least the catch-all, so none of them computes ServeMux's own Allow set.
func (rt *router) allowed(r *http.Request) []string {
	probe := *r // ServeMux.Handler reads only Method, URL and Host
	var allow []string
	for _, method := range probeMethods {
		if method == r.Method {
			continue
		}
		probe.Method = method
		if _, pattern := rt.mux.Handler(
			&probe,
		); pattern != "" && pattern != catchAll &&
			!rt.headOnGet(method, pattern) {
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
