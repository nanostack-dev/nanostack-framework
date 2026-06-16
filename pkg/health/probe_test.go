package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/health"
)

func TestProbeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantError bool
	}{
		{name: "healthy 200", status: http.StatusOK, wantError: false},
		{name: "healthy 204", status: http.StatusNoContent, wantError: false},
		{name: "unhealthy 503", status: http.StatusServiceUnavailable, wantError: true},
		{name: "unhealthy 500", status: http.StatusInternalServerError, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			err := health.ProbeURLForTest(context.Background(), srv.URL)
			if tc.wantError && err == nil {
				t.Fatalf("expected error for status %d, got nil", tc.status)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected no error for status %d, got %v", tc.status, err)
			}
		})
	}
}

func TestProbeURLUnreachable(t *testing.T) {
	t.Parallel()

	// Closed server address -> connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := health.ProbeURLForTest(context.Background(), url); err == nil {
		t.Fatal("expected error probing a closed server, got nil")
	}
}
