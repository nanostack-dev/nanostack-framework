// Package log sets up log/slog for Nanostack services. log/slog is the only
// logging API: call sites use slog.InfoContext(ctx, ...) or an injected
// *slog.Logger, and this package supplies what the standard library leaves to
// the application:
//
//   - NewHandler builds the process handler: JSON in production, readable text
//     in development, source locations, an optional export handler (OTLP
//     through otelslog) fanned out with slog.NewMultiHandler, and optional
//     attributes read from the context, such as the trace_id and span_id that
//     tracing.LogAttrs returns. The package itself depends only on the
//     standard library.
//   - WithScope, SetAttrs and WithAttrs carry attributes (request id,
//     organization, user, component) on the context. The handler reads them
//     when a line is written, so no call site passes them and no custom logger
//     type exists.
//   - secret.Secret and the key guard keep credentials out of logs.
//   - HTTPMiddleware opens the request scope and writes one summary line per
//     request.
//   - Error logs an error at the level its kind deserves.
//
// Wire it once at startup:
//
//	cfg, err := log.ConfigFromEnv(os.Getenv)
//	if err != nil { ... }
//	slog.SetDefault(log.New(cfg))
//
// Tests use the logtest subpackage.
package log

import (
	"context"
	"log/slog"
	"slices"
)

// NewHandler returns the process handler that cfg describes.
//
// Records flow through three stages. The context stage adds the attributes of
// WithScope, SetAttrs and WithAttrs. The redaction stage replaces the value of
// every attribute whose key looks secret (see secret.IsKey). The fan-out stage sends
// the record to the output handler, which also gets cfg.ContextAttrs (the
// trace ids) and the service identity, and to cfg.Export when set. The export
// handler gets neither: an OpenTelemetry handler reads the span from the
// context and the service from its resource, so adding them again would
// duplicate them.
func NewHandler(cfg Config) slog.Handler {
	output := newOutputHandler(cfg)
	if identity := cfg.identity(); len(identity) > 0 {
		output = output.WithAttrs(identity)
	}
	sink := output
	if cfg.ContextAttrs != nil {
		sink = newContextHandler(output, cfg.ContextAttrs)
	}
	if cfg.Export != nil {
		sink = slog.NewMultiHandler(sink, cfg.Export)
	}
	return newContextHandler(newRedactHandler(sink), Attrs)
}

// New returns a logger on NewHandler(cfg). Install it with slog.SetDefault so
// package-level slog calls and the standard library's log package use it.
func New(cfg Config) *slog.Logger {
	return slog.New(NewHandler(cfg))
}

// Wrap adds the framework's context attributes and redaction to h, and the
// attributes each of contextAttrs reads from the context, such as
// tracing.LogAttrs. Use it when a handler is built outside NewHandler, as
// logtest does, so that what a test sees matches production.
func Wrap(h slog.Handler, contextAttrs ...func(context.Context) []slog.Attr) slog.Handler {
	extract := viewAttrs
	if len(contextAttrs) > 0 {
		extract = func(ctx context.Context) []slog.Attr {
			attrs := viewAttrs(ctx)
			for _, f := range contextAttrs {
				attrs = merge(attrs, f(ctx))
			}
			return attrs
		}
	}
	return newContextHandler(newRedactHandler(h), extract)
}

// contextHandler adds the attributes that extract reads from the context to
// every record.
//
// The attributes always land at the top level, even under Logger.WithGroup: a
// request id nested inside a group would escape a query on request_id. The
// handler therefore keeps base, the handler before the first WithGroup, and
// the operations applied after it. A record from a grouped logger is handled
// by base with the context attributes added, then the operations replayed.
// That costs a handler rebuild per record, paid only by grouped loggers on a
// context that carries attributes.
//
// A key that Logger.With set at the top level is written once: the handler
// drops a context attribute with the same key, and the logger's value stays.
// The logger's attributes are formatted into the inner handler when the
// logger is built, so they cannot give way to the context per record; and a
// logger that names its component should keep it on every line, as a zerolog
// logger, which never read the context, did.
type contextHandler struct {
	base    slog.Handler // inner handler before the first WithGroup
	inner   slog.Handler // base with ops applied
	ops     []handlerOp
	keys    []string // top-level keys set with WithAttrs before the first WithGroup
	extract func(context.Context) []slog.Attr
}

// handlerOp is one WithGroup or WithAttrs call made after the first group.
type handlerOp struct {
	group string
	attrs []slog.Attr
}

func newContextHandler(inner slog.Handler, extract func(context.Context) []slog.Attr) *contextHandler {
	return &contextHandler{base: inner, inner: inner, extract: extract}
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := withoutKeys(h.extract(ctx), h.keys)
	if len(attrs) == 0 {
		return h.inner.Handle(ctx, r)
	}
	if len(h.ops) == 0 {
		return h.inner.Handle(ctx, prepend(r, attrs))
	}
	// The record's own attributes sit inside the groups, so they cannot
	// collide with the top-level context attributes.
	handler := h.base.WithAttrs(attrs)
	for _, op := range h.ops {
		if op.group != "" {
			handler = handler.WithGroup(op.group)
		} else {
			handler = handler.WithAttrs(op.attrs)
		}
	}
	return handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	if len(h.ops) == 0 {
		next := h.base.WithAttrs(attrs)
		keys := topLevelKeys(slices.Clip(h.keys), attrs)
		return &contextHandler{base: next, inner: next, keys: keys, extract: h.extract}
	}
	// After a group, the attributes sit inside it and cannot collide with the
	// top-level context attributes.
	return &contextHandler{
		base:    h.base,
		inner:   h.inner.WithAttrs(attrs),
		ops:     append(slices.Clip(h.ops), handlerOp{attrs: attrs}),
		keys:    h.keys,
		extract: h.extract,
	}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &contextHandler{
		base:    h.base,
		inner:   h.inner.WithGroup(name),
		ops:     append(slices.Clip(h.ops), handlerOp{group: name}),
		keys:    h.keys,
		extract: h.extract,
	}
}

// topLevelKeys appends to keys the top-level keys that attrs write: each key,
// and the keys inside a group with an empty key, which slog inlines.
func topLevelKeys(keys []string, attrs []slog.Attr) []string {
	for _, a := range attrs {
		switch {
		case a.Key == "" && a.Value.Kind() == slog.KindGroup:
			keys = topLevelKeys(keys, a.Value.Group())
		case a.Key != "" && !slices.Contains(keys, a.Key):
			keys = append(keys, a.Key)
		}
	}
	return keys
}

// withoutKeys returns attrs minus those whose key is in keys. It returns attrs
// itself when nothing is removed, and a new slice otherwise: attrs may be a
// view of a scope, which must not be written.
func withoutKeys(attrs []slog.Attr, keys []string) []slog.Attr {
	if len(keys) == 0 {
		return attrs
	}
	var kept []slog.Attr
	for i, a := range attrs {
		switch {
		case slices.Contains(keys, a.Key):
			if kept == nil {
				kept = append(make([]slog.Attr, 0, len(attrs)-1), attrs[:i]...)
			}
		case kept != nil:
			kept = append(kept, a)
		}
	}
	if kept == nil {
		return attrs
	}
	return kept
}

// prepend returns a record with attrs before r's own attributes. A key the call
// site also logs is left to the call site: it is the more specific value, and
// emitting both would duplicate the key.
func prepend(r slog.Record, attrs []slog.Attr) slog.Record {
	// One slice and one AddAttrs, so the record grows once.
	kept := withoutRecordKeys(attrs, r)
	all := make([]slog.Attr, 0, len(kept)+r.NumAttrs())
	all = append(all, kept...)
	r.Attrs(func(a slog.Attr) bool {
		all = append(all, a)
		return true
	})
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(all...)
	return out
}

// withoutRecordKeys returns attrs minus those whose key is a top-level key of
// r. It returns attrs itself when nothing is removed.
func withoutRecordKeys(attrs []slog.Attr, r slog.Record) []slog.Attr {
	if r.NumAttrs() == 0 {
		return attrs
	}
	keep := attrs
	copied := false
	r.Attrs(func(a slog.Attr) bool {
		i := slices.IndexFunc(keep, func(c slog.Attr) bool { return c.Key == a.Key })
		if i < 0 {
			return true
		}
		if !copied {
			keep = slices.Clone(keep)
			copied = true
		}
		keep = slices.Delete(keep, i, i+1)
		return true
	})
	return keep
}
