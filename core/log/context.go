package log

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// Keys the framework writes. Products use the same keys for the same facts, so
// one log query finds every line about a request, tenant or user.
const (
	KeyRequestID   = "request_id"
	KeyComponent   = "component"
	KeyOrgID       = "org_id"
	KeyWorkspaceID = "workspace_id"
	KeyUserID      = "user_id"
	KeyService     = "service" // the process identity, from Config.Service
	KeyError       = "error"
	KeyCause       = "cause"   // why work stopped when that is not a failure, such as a shutdown's context cause
	KeyStatus      = "status"  // HTTP status of a response or an error
	KeyProblem     = "problem" // RFC 9457 problem type URI of an error response
	KeyRoute       = "route"   // the ServeMux pattern that matched the request, such as "GET /flows/{id}"
	KeyTraceID     = "trace_id"
	KeySpanID      = "span_id"
)

// Keys of the request summary line that HTTPMiddleware writes. The log
// collector renames KeyMethod and KeyPath, and the alerts read KeyStatus and
// KeyDuration, so these names are a contract: a test pins them.
const (
	KeyMethod         = "method"
	KeyPath           = "path"
	KeyDuration       = "duration"
	KeyBytesOut       = "bytes_out"
	KeyClientIP       = "client_ip"
	KeyQuery          = "query"
	KeyUserAgent      = "user_agent"
	KeyClientCanceled = "client_canceled"
	KeyHijacked       = "hijacked"
	KeyPanic          = "panic"
)

type (
	attrsKey struct{}
	scopeKey struct{}
)

// scope is the mutable attribute set of one unit of work: an HTTP request, a
// queue job, a cron tick. Code deep in the call tree adds to it with SetAttrs,
// and every line logged with a context derived from the scope reads it,
// including lines written by middleware that holds an older *http.Request.
type scope struct {
	mu    sync.RWMutex
	attrs []slog.Attr
}

// WithScope returns a context that carries a new, empty scope for one unit of
// work. A scope already on ctx is copied, so the new one starts with its
// attributes and later SetAttrs calls on either do not affect the other.
//
// HTTPMiddleware opens a scope per request. Queue workers and crons open one
// per job or tick.
func WithScope(ctx context.Context) context.Context {
	next := &scope{}
	if parent, ok := ctx.Value(scopeKey{}).(*scope); ok {
		parent.mu.RLock()
		next.attrs = slices.Clone(parent.attrs)
		parent.mu.RUnlock()
	}
	return context.WithValue(ctx, scopeKey{}, next)
}

// SetAttrs adds attrs to the scope on ctx and reports whether ctx has a scope.
// An attribute replaces an earlier one with the same key.
//
// Use it for facts that belong to the whole unit of work and are learned
// partway through it, such as the organization an auth middleware resolves.
// The request summary line that HTTPMiddleware writes after the handler
// returns then carries them too. Without a scope, SetAttrs does nothing and
// returns false; use WithAttrs there.
func SetAttrs(ctx context.Context, attrs ...slog.Attr) bool {
	s, ok := ctx.Value(scopeKey{}).(*scope)
	if !ok {
		return false
	}
	s.mu.Lock()
	s.attrs = merge(s.attrs, attrs)
	s.mu.Unlock()
	return true
}

// WithAttrs returns a context whose log lines carry attrs. An attribute
// replaces an earlier one with the same key, so calling WithAttrs again with a
// new value is safe and never emits a duplicate key.
//
// Use it for facts that narrow one branch of work: the component of a
// background loop, the job id of one queue message, the tenant of one row in a
// cron. Contexts derived from the result see the attributes; the parent does
// not.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	current, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return context.WithValue(ctx, attrsKey{}, merge(slices.Clip(current), attrs))
}

// Attrs returns the attributes that a line logged with ctx carries: the scope's
// attributes, then those added with WithAttrs. When both have a key, the
// WithAttrs value wins because it is the more specific one. The result is a new
// slice that the caller may keep.
func Attrs(ctx context.Context) []slog.Attr {
	var out []slog.Attr
	if s, ok := ctx.Value(scopeKey{}).(*scope); ok {
		s.mu.RLock()
		out = slices.Clone(s.attrs)
		s.mu.RUnlock()
	}
	if layered, ok := ctx.Value(attrsKey{}).([]slog.Attr); ok {
		out = merge(out, layered)
	}
	return out
}

// RequestID returns the request id that HTTPMiddleware put on ctx, or "" off
// the HTTP path. Pass it to outbound calls and job payloads to correlate them.
func RequestID(ctx context.Context) string {
	for _, a := range Attrs(ctx) {
		if a.Key == KeyRequestID {
			return a.Value.String()
		}
	}
	return ""
}

// merge appends add to base, replacing attributes of base whose key appears in
// add. It may reuse base's backing array; callers pass a clipped or owned
// slice.
func merge(base, add []slog.Attr) []slog.Attr {
	for _, a := range add {
		if a.Key == "" {
			continue
		}
		if i := slices.IndexFunc(base, func(b slog.Attr) bool { return b.Key == a.Key }); i >= 0 {
			base = slices.Clone(base)
			base[i] = a
			continue
		}
		base = append(base, a)
	}
	return base
}
