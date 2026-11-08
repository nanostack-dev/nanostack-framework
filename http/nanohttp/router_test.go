package nanohttp_test

import (
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Pattern+" "+r.PathValue("id"))
	})
	mux.HandleFunc("DELETE /flows/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /tree/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tree")
	})
	mux.HandleFunc("/any", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "any")
	})
	return mux
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// statusHandler answers with status and a marker body, to show which handler ran.
func statusHandler(status int, marker string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, marker)
	})
}

// TestServeMuxHandlerProperties pins the ServeMux.Handler behaviour that
// NewRouter relies on. If a Go release changes it, this test fails before the
// router misroutes.
func TestServeMuxHandlerProperties(t *testing.T) {
	t.Parallel()
	mux := routes()
	tests := []struct {
		name, method, target string
		wantPattern          string
		wantRedirect         bool
	}{
		{"match", http.MethodGet, "/health", "GET /health", false},
		{"HEAD matches a GET pattern", http.MethodHead, "/health", "GET /health", false},
		{"method mismatch is empty", http.MethodPost, "/health", "", false},
		{"no match is empty", http.MethodGet, "/nothing", "", false},
		{"trailing-slash redirect names the pattern", http.MethodGet, "/tree", "GET /tree/", true},
		{"clean-path redirect to a match names the pattern", http.MethodGet, "/x/../health", "GET /health", true},
		{"clean-path redirect without a match is empty", http.MethodGet, "//nothing", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, pattern := mux.Handler(httptest.NewRequest(tt.method, tt.target, nil))
			if pattern != tt.wantPattern {
				t.Fatalf("pattern = %q, want %q", pattern, tt.wantPattern)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			if redirect := rec.Header().Get("Location") != ""; redirect != tt.wantRedirect {
				t.Fatalf("redirect = %v (status %d), want %v", redirect, rec.Code, tt.wantRedirect)
			}
		})
	}
}

func TestRouterServesMatchesThroughTheMux(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{ExplicitHEAD: true})
	if rec := do(
		rt,
		http.MethodGet,
		"/flows/f1",
	); rec.Code != http.StatusOK ||
		rec.Body.String() != "GET /flows/{id} f1" {
		t.Fatalf("GET /flows/f1 = %d %q, want the pattern and path value", rec.Code, rec.Body)
	}
	if rec := do(rt, http.MethodHead, "/any"); rec.Code != http.StatusOK {
		t.Fatalf("HEAD on a pattern without a method = %d, want 200", rec.Code)
	}
}

func TestRouterHEADOnGETPattern(t *testing.T) {
	t.Parallel()
	t.Run("default: ServeMux serves HEAD with the GET handler", func(t *testing.T) {
		t.Parallel()
		rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{})
		if rec := do(rt, http.MethodHead, "/health"); rec.Code != http.StatusOK {
			t.Fatalf("HEAD /health = %d, want 200", rec.Code)
		}
	})
	t.Run("ExplicitHEAD: 405 as chi", func(t *testing.T) {
		t.Parallel()
		rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{
			ExplicitHEAD: true, MethodNotAllowed: statusHandler(http.StatusMethodNotAllowed, "custom 405"),
		})
		rec := do(rt, http.MethodHead, "/health")
		if rec.Code != http.StatusMethodNotAllowed || rec.Body.String() != "custom 405" {
			t.Fatalf("HEAD /health = %d %q, want the custom 405", rec.Code, rec.Body)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET" {
			t.Fatalf("Allow = %q, want GET without HEAD", allow)
		}
	})
}

// TestRouterHealthGateMethods replays the echopoint gate assertions on
// /health: every method but GET answers 405.
func TestRouterHealthGateMethods(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{ExplicitHEAD: true})
	for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodPatch, http.MethodOptions, http.MethodDelete} {
		if rec := do(rt, method, "/health"); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /health = %d, want 405", method, rec.Code)
		}
	}
}

func TestRouterMethodNotAllowedSetsAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		explicitHEAD bool
		target       string
		want         string
	}{
		{false, "/health", "GET, HEAD"},
		{true, "/health", "GET"},
		{false, "/flows/f1", "DELETE, GET, HEAD"},
		{false, "/tree", "GET, HEAD"}, // the trailing-slash pattern counts, as in ServeMux
	}
	for _, tt := range tests {
		rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{ExplicitHEAD: tt.explicitHEAD})
		rec := do(rt, http.MethodPost, tt.target)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != tt.want {
			t.Errorf("explicitHEAD=%v POST %s = %d Allow %q, want 405 Allow %q",
				tt.explicitHEAD, tt.target, rec.Code, rec.Header().Get("Allow"), tt.want)
		}
	}
	// Without ExplicitHEAD, Allow is the one ServeMux writes itself.
	bare := do(routes(), http.MethodPost, "/flows/f1")
	if routed := do(
		nanohttp.NewRouter(routes(), nanohttp.RouterOptions{}),
		http.MethodPost,
		"/flows/f1",
	); routed.Header().
		Get("Allow") !=
		bare.Header().
			Get("Allow") {
		t.Fatalf("Allow = %q, ServeMux writes %q", routed.Header().Get("Allow"), bare.Header().Get("Allow"))
	}
}

func TestRouterDefaultProblems(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{})
	for _, tt := range []struct {
		method, target string
		status         int
		typ            string
	}{
		{http.MethodGet, "/nothing", http.StatusNotFound, problem.TypeNotFound},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed, problem.TypeMethodNotAllowed},
	} {
		rec := do(rt, tt.method, tt.target)
		if rec.Code != tt.status || rec.Header().Get("Content-Type") != problem.ContentType {
			t.Fatalf("%s %s = %d %s", tt.method, tt.target, rec.Code, rec.Header().Get("Content-Type"))
		}
		var p problem.Problem
		if err := jsonv2.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Type != tt.typ || p.Status != tt.status {
			t.Fatalf("%s %s body = %s (%v)", tt.method, tt.target, rec.Body, err)
		}
	}
}

func TestRouterCustomNotFound(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(
		routes(),
		nanohttp.RouterOptions{NotFound: statusHandler(http.StatusBadRequest, "custom 404")},
	)
	for _, target := range []string{"/nothing", "/flows/f1/x"} {
		rec := do(rt, http.MethodGet, target)
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "custom 404" || rec.Header().Get("Allow") != "" {
			t.Errorf(
				"GET %s = %d %q Allow %q, want the custom handler",
				target,
				rec.Code,
				rec.Body,
				rec.Header().Get("Allow"),
			)
		}
	}
}

// TestRouterKeepsServeMuxRedirects checks that a redirect to a match is the
// one ServeMux writes, status and Location.
func TestRouterKeepsServeMuxRedirects(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{ExplicitHEAD: true})
	for _, target := range []string{"/tree", "/x/../health", "/tree?q=1"} {
		want := do(routes(), http.MethodGet, target)
		got := do(rt, http.MethodGet, target)
		if got.Code != want.Code || got.Header().Get("Location") != want.Header().Get("Location") ||
			want.Header().Get("Location") == "" {
			t.Errorf(
				"GET %s = %d %q, ServeMux %d %q",
				target,
				got.Code,
				got.Header().Get("Location"),
				want.Code,
				want.Header().Get("Location"),
			)
		}
	}
}

// TestRouterUncleanPathsKeepServeMuxRedirect checks that a request with an
// unclean path gets the redirect of a bare ServeMux for every method, also
// when the clean path matches a pattern only for other methods (POST //health
// is a 307 to /health, not a 405) or no pattern at all (//nothing).
func TestRouterUncleanPathsKeepServeMuxRedirect(t *testing.T) {
	t.Parallel()
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodTrace,
	}
	targets := []string{"//health", "/a/../health", "/./health", "//nothing", "/flows//f1", "//health?x=1"}
	for _, explicitHEAD := range []bool{false, true} {
		rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{
			ExplicitHEAD:     explicitHEAD,
			NotFound:         statusHandler(http.StatusTeapot, "custom 404"),
			MethodNotAllowed: statusHandler(http.StatusTeapot, "custom 405"),
		})
		for _, method := range methods {
			for _, target := range targets {
				want := do(routes(), method, target)
				got := do(rt, method, target)
				if want.Code != http.StatusTemporaryRedirect {
					t.Fatalf(
						"ServeMux %s %s = %d, want 307: the pinned ServeMux behaviour changed",
						method,
						target,
						want.Code,
					)
				}
				if got.Code != want.Code || got.Header().Get("Location") != want.Header().Get("Location") ||
					got.Header().Get("Allow") != "" {
					t.Errorf("ExplicitHEAD=%v %s %s = %d Location %q Allow %q, ServeMux %d Location %q",
						explicitHEAD, method, target, got.Code, got.Header().Get("Location"),
						got.Header().Get("Allow"), want.Code, want.Header().Get("Location"))
				}
			}
		}
	}
}

func TestRouterAsteriskRequestGoesToServeMux(t *testing.T) {
	t.Parallel()
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{NotFound: statusHandler(http.StatusTeapot, "")})
	req := httptest.NewRequest(http.MethodOptions, "*", nil)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("OPTIONS * = %d, want ServeMux's 400", rec.Code)
	}
}

func BenchmarkRouterMatch(b *testing.B) {
	rt := nanohttp.NewRouter(routes(), nanohttp.RouterOptions{ExplicitHEAD: true})
	mux := routes()
	req := httptest.NewRequest(http.MethodGet, "/flows/f1", nil)
	b.Run("router", func(b *testing.B) {
		for b.Loop() {
			rt.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
	b.Run("servemux", func(b *testing.B) {
		for b.Loop() {
			mux.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}
