package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(Config{Service: "svc", Version: "v1", CommitSHA: "abc"}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var payload map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["service"] != "svc" {
		t.Fatalf("unexpected service: %+v", payload)
	}
}

func TestDynamicRegistry(t *testing.T) {
	t.Run("Healthy Checks", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(NewChecker("db", func(ctx context.Context) error {
			return nil
		}))
		reg.Register(NewChecker("redis", func(ctx context.Context) error {
			return nil
		}))

		recorder := httptest.NewRecorder()
		handler := NewHandler(Config{
			Service:  "svc",
			Registry: reg,
		})
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", recorder.Code)
		}

		var payload map[string]any
		if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if payload["db"] != "HEALTHY" || payload["redis"] != "HEALTHY" || payload["status"] != "HEALTHY" {
			t.Fatalf("expected healthy checks, got: %+v", payload)
		}
	})

	t.Run("Unhealthy Check", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(NewChecker("db", func(ctx context.Context) error {
			return nil
		}))
		reg.Register(NewChecker("redis", func(ctx context.Context) error {
			return errors.New("connection pool exhausted")
		}))

		recorder := httptest.NewRecorder()
		handler := NewHandler(Config{
			Service:  "svc",
			Registry: reg,
		})
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", recorder.Code)
		}

		var payload map[string]any
		if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if payload["db"] != "HEALTHY" || payload["redis"] != "UNHEALTHY: connection pool exhausted" || payload["status"] != "UNHEALTHY" {
			t.Fatalf("expected unhealthy status, got: %+v", payload)
		}
	})
}
