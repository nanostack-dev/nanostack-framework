package log_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// wrapper is a middleware writer between HTTPMiddleware and the handler.
type wrapper struct{ http.ResponseWriter }

func (w wrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestResponseStatusFollowsTheMiddlewareThroughWrappers(t *testing.T) {
	if _, _, known := log.ResponseStatus(httptest.NewRecorder()); known {
		t.Fatal("a writer without HTTPMiddleware must be unknown")
	}
	type seen struct {
		status        int
		written, know bool
	}
	var before, after seen
	handler := log.HTTPMiddleware(log.HTTPOptions{Logger: slog.New(slog.DiscardHandler)})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			inner := wrapper{w}
			before.status, before.written, before.know = log.ResponseStatus(inner)
			inner.WriteHeader(http.StatusAccepted)
			after.status, after.written, after.know = log.ResponseStatus(inner)
		}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !before.know || before.written {
		t.Fatalf("before WriteHeader = %+v, want known and not written", before)
	}
	if after != (seen{http.StatusAccepted, true, true}) {
		t.Fatalf("after WriteHeader = %+v, want 202 written", after)
	}
}
