package example_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/internal/example"
)

// handlers.gen.go splits StrictServerInterface by OpenAPI tag: one
// interface per tag, and Handlers, which passes each request to the
// handler of its tag (ADR 0024).
var _ example.StrictServerInterface = (*example.Handlers)(nil)

// notes serves the Notes tag and counts its calls.
type notes struct{ calls *int }

func (n notes) AddNote(context.Context, example.AddNoteRequestObject) (example.AddNoteResponseObject, error) {
	*n.calls++
	return example.AddNote204Response{}, nil
}

// Each request reaches the handler of its operation's tag.
func TestHandlersPassEachRequestToItsTag(t *testing.T) {
	var calls int
	_, webhooks := newServer()
	h, err := example.NewHandlers(notes{calls: &calls}, webhooks)
	if err != nil {
		t.Fatal(err)
	}
	srv := example.NewHTTPHandler(h, example.HTTPHandlerOptions{})

	req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || calls != 1 {
		t.Errorf("POST /notes = %d with %d Notes calls, want 204 with 1", rec.Code, calls)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks/wh_1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"wh_1"`) || calls != 1 {
		t.Errorf("GET /webhooks/wh_1 = %d %s with %d Notes calls, want 200 from the Webhooks handler",
			rec.Code, rec.Body.String(), calls)
	}
}

// NewHandlers refuses a nil handler and names its tag.
func TestNewHandlersRefusesANilHandler(t *testing.T) {
	var calls int
	_, webhooks := newServer()
	for _, tt := range []struct {
		name     string
		notes    example.NotesServer
		webhooks example.WebhooksServer
		want     string
	}{
		{"notes", nil, webhooks, "NewHandlers: the handler of tag Notes is nil"},
		{"webhooks", notes{calls: &calls}, nil, "NewHandlers: the handler of tag Webhooks is nil"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, err := example.NewHandlers(tt.notes, tt.webhooks)
			if h != nil || err == nil || err.Error() != tt.want {
				t.Errorf("NewHandlers = %v, %v; want nil, %q", h, err, tt.want)
			}
		})
	}
}
