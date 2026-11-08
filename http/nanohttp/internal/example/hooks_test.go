package example_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/internal/example"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// hookCall records which error hook ran and with which error.
type hookCall struct {
	hook string
	err  error
}

// TestHTTPHandlerOptionsReplaceEveryErrorPath checks the error seam that a
// legacy envelope writer plugs into: with both hooks set, every error the
// generated server produces reaches one of them, and parameter binding errors
// arrive as problems that name the parameter.
func TestHTTPHandlerOptionsReplaceEveryErrorPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, method, target, body string
		wantHook                   string
		check                      func(error) bool
	}{
		{
			"parameter binding", http.MethodGet, "/webhooks/wh_1/events?limit=abc", "", "request",
			func(err error) bool { return paramName(err) == "limit" },
		},
		{
			"parameter validation", http.MethodGet, "/webhooks/wh_1/events?limit=101", "", "request",
			func(err error) bool { return paramName(err) == "limit" },
		},
		{
			"malformed body", http.MethodPost, "/webhooks", "{", "request",
			func(err error) bool { return code(err) == "http.malformed_json" },
		},
		{
			"body validation", http.MethodPost, "/webhooks", `{}`, "request",
			func(err error) bool { return code(err) == "http.validation_failed" },
		},
		{
			"handler error", http.MethodGet, "/webhooks/wh_2", "", "response",
			func(err error) bool { return errors.As(err, new(notFound)) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls []hookCall
			hook := func(name string) func(http.ResponseWriter, *http.Request, error) {
				return func(w http.ResponseWriter, _ *http.Request, err error) {
					calls = append(calls, hookCall{name, err})
					w.WriteHeader(http.StatusTeapot)
				}
			}
			h := example.NewHTTPHandler(
				server{gotBody: make(chan *example.UpdateWebhookRequest, 1)},
				example.HTTPHandlerOptions{
					RequestErrorHandlerFunc:  hook("request"),
					ResponseErrorHandlerFunc: hook("response"),
				},
			)
			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusTeapot || len(calls) != 1 {
				t.Fatalf("status %d, calls %v: want one hook call", rec.Code, calls)
			}
			if calls[0].hook != tt.wantHook || !tt.check(calls[0].err) {
				t.Fatalf("hook %s got %v, want the %s hook", calls[0].hook, calls[0].err, tt.wantHook)
			}
		})
	}
}

func paramName(err error) string {
	p, ok := errors.AsType[*problem.Error](err)
	if !ok || len(p.FieldErrors()) == 0 {
		return ""
	}
	return p.FieldErrors()[0].Parameter
}

func code(err error) string {
	if p, ok := errors.AsType[*problem.Error](err); ok {
		return p.ProblemCode()
	}
	return ""
}
