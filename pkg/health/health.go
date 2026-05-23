package health

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

const (
	DefaultPath        = "/health"
	defaultEnvironment = "development"
)

// Checker defines the contract for dynamic health checks.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// DependencyChecker is a dynamic health checker function wrapper.
type DependencyChecker struct {
	name string
	fn   func(context.Context) error
}

func (d DependencyChecker) Name() string { return d.name }
func (d DependencyChecker) Check(ctx context.Context) error { return d.fn(ctx) }

// NewChecker wraps a check function into a Checker interface.
func NewChecker(name string, fn func(context.Context) error) Checker {
	return DependencyChecker{name: name, fn: fn}
}

// Registry aggregates individual dynamic dependency health checkers.
type Registry struct {
	checkers []Checker
}

// NewRegistry constructs an empty health check registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a dynamic dependency checker to the registry.
func (r *Registry) Register(c Checker) {
	if r == nil {
		return
	}
	r.checkers = append(r.checkers, c)
}

// Evaluate runs all registered checks in parallel with a strict timeout and returns results and overall status.
func (r *Registry) Evaluate(ctx context.Context) (map[string]any, bool) {
	results := make(map[string]any)
	allHealthy := true
	if r == nil || len(r.checkers) == 0 {
		return results, allHealthy
	}

	type checkResult struct {
		name string
		err  error
	}
	ch := make(chan checkResult, len(r.checkers))

	for _, checker := range r.checkers {
		go func(c Checker) {
			ctxWithTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			err := c.Check(ctxWithTimeout)
			ch <- checkResult{name: c.Name(), err: err}
		}(checker)
	}

	for range r.checkers {
		res := <-ch
		if res.err != nil {
			results[res.name] = "UNHEALTHY: " + res.err.Error()
			allHealthy = false
		} else {
			results[res.name] = "HEALTHY"
		}
	}

	return results, allHealthy
}

// Response is the standard public health response payload.
type Response struct {
	Status      string         `json:"status"`
	Service     string         `json:"service"`
	Environment string         `json:"environment"`
	Version     string         `json:"version"`
	CommitSHA   string         `json:"commit_sha"`
	BuildDate   *string        `json:"build_date,omitempty"`
	Extra       map[string]any `json:"-"`
}

// Config configures the standard health response.
type Config struct {
	Service   string
	Version   string
	CommitSHA string
	BuildDate string
	Status    string
	Extra     map[string]any
	Registry  *Registry
}

// Mount registers the standard health endpoint on mux.
func Mount(mux interface {
	Get(string, http.HandlerFunc)
}, cfg Config) {
	handler := NewHandler(cfg)
	mux.Get(DefaultPath, handler.ServeHTTP)
}

// NewHandler builds a standard health handler.
func NewHandler(cfg Config) http.Handler {
	status := cfg.Status
	if status == "" {
		status = "HEALTHY"
	}
	return Handler{
		response: Response{
			Status:      status,
			Service:     cfg.Service,
			Environment: CurrentEnvironment(),
			Version:     cfg.Version,
			CommitSHA:   cfg.CommitSHA,
			BuildDate:   stringPtr(cfg.BuildDate),
			Extra:       cfg.Extra,
		},
		registry: cfg.Registry,
	}
}

// CurrentEnvironment resolves the environment string used by health responses.
func CurrentEnvironment() string {
	if value := os.Getenv("ENVIRONMENT"); value != "" {
		return value
	}
	return defaultEnvironment
}

// Handler serves the configured health payload.
type Handler struct {
	response Response
	registry *Registry
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":      h.response.Status,
		"service":     h.response.Service,
		"environment": h.response.Environment,
		"version":     h.response.Version,
		"commit_sha":  h.response.CommitSHA,
	}
	if h.response.BuildDate != nil {
		payload["build_date"] = *h.response.BuildDate
	}

	status := http.StatusOK
	if h.registry != nil {
		extra, healthy := h.registry.Evaluate(r.Context())
		for k, v := range extra {
			payload[k] = v
		}
		if !healthy {
			payload["status"] = "UNHEALTHY"
			status = http.StatusServiceUnavailable
		}
	}

	for key, value := range h.response.Extra {
		payload[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
