package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name   string
		status int
		value  any
	}{
		{name: "ok with map", status: http.StatusOK, value: map[string]any{"id": "abc"}},
		{name: "created with struct", status: http.StatusCreated, value: struct {
			Name string `json:"name"`
		}{Name: "svc"}},
		{name: "service unavailable with nil", status: http.StatusServiceUnavailable, value: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			if err := WriteJSON(recorder, tt.status, tt.value); err != nil {
				t.Fatalf("WriteJSON returned error: %v", err)
			}

			if recorder.Code != tt.status {
				t.Fatalf("expected status %d, got %d", tt.status, recorder.Code)
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("expected application/json content type, got %q", contentType)
			}

			wantBody, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("marshal expected value: %v", err)
			}
			if got := recorder.Body.String(); got != string(wantBody)+"\n" {
				t.Fatalf("unexpected body: got %q, want %q", got, string(wantBody)+"\n")
			}
		})
	}
}

func TestWriteJSONReturnsEncodeError(t *testing.T) {
	recorder := httptest.NewRecorder()

	if err := WriteJSON(recorder, http.StatusOK, make(chan int)); err == nil {
		t.Fatal("expected an error for an unencodable value")
	}
}
