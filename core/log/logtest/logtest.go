// Package logtest captures log records in tests and asserts on them.
//
// Each test gets its own Recorder, so tests stay safe under t.Parallel, and
// the Recorder starts no goroutines and never waits, so it works inside a
// testing/synctest bubble. Records pass through log.Wrap first: context
// attributes and redaction behave as in production. Trace ids are not added,
// since core/log does not depend on OpenTelemetry.
//
// Code that takes a *slog.Logger gets the one New returns:
//
//	logger, rec := logtest.New()
//	svc := NewService(logger)
//	svc.Run(t.Context())
//	rec.AssertLogged(t, slog.LevelWarn, "queue is backing up", slog.String("queue", "jobs"))
//
// Code that logs through the package-level slog functions with a context gets
// the context Capture returns:
//
//	ctx, rec := logtest.Capture(t)
//	svc.Run(ctx)
//	rec.AssertLogged(t, slog.LevelError, "load flow")
package logtest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// Entry is one captured record. Attrs holds every attribute, resolved, with a
// group member under its dotted path ("http.status") and the context
// attributes at the top level.
type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
	// PC is the record's program counter; Source decodes it.
	PC uintptr
}

// Value returns the value of the attribute at key, a dotted path for group
// members.
func (e Entry) Value(key string) (slog.Value, bool) {
	for _, a := range e.Attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return slog.Value{}, false
}

// String returns the value at key formatted as text, or "" when absent.
func (e Entry) String(key string) string {
	v, ok := e.Value(key)
	if !ok {
		return ""
	}
	return v.String()
}

// Source returns the function, file and line that logged the record, or nil
// when the record has none.
func (e Entry) Source() *slog.Source {
	if e.PC == 0 {
		return nil
	}
	frame, _ := runtime.CallersFrames([]uintptr{e.PC}).Next()
	return &slog.Source{Function: frame.Function, File: frame.File, Line: frame.Line}
}

// Format renders e on one line for failure messages.
func (e Entry) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q", e.Level, e.Message)
	for _, a := range e.Attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
	}
	return b.String()
}

// Recorder stores captured entries. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	entries []Entry
	handler slog.Handler // log.Wrap around the capturing handler
}

func newRecorder() *Recorder {
	rec := &Recorder{}
	rec.handler = log.Wrap(&captureHandler{rec: rec})
	return rec
}

// New returns a logger that records every level into a new Recorder.
func New() (*slog.Logger, *Recorder) {
	rec := newRecorder()
	return slog.New(rec.handler), rec
}

// Handler returns the Recorder's handler, for code that builds its own
// *slog.Logger or composes handlers.
func (r *Recorder) Handler() slog.Handler {
	return r.handler
}

// Entries returns a copy of the captured entries, oldest first.
func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.entries)
}

// Reset drops the captured entries.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.entries = nil
	r.mu.Unlock()
}

// Find returns the entries with level and msg whose attributes include every
// attr in want. An attr matches by key (a dotted path for group members) and
// value; a group in want matches member by member. An error value matches an
// entry error it errors.Is.
func (r *Recorder) Find(level slog.Level, msg string, want ...slog.Attr) []Entry {
	expected := flatten(nil, "", want)
	var found []Entry
	for _, e := range r.Entries() {
		if e.Level == level && e.Message == msg && hasAll(e, expected) {
			found = append(found, e)
		}
	}
	return found
}

// AssertLogged fails t unless exactly one entry matches Find(level, msg,
// want...), and returns it. Exactly one, because a line logged twice is
// usually a bug too.
func (r *Recorder) AssertLogged(t testing.TB, level slog.Level, msg string, want ...slog.Attr) Entry {
	t.Helper()
	found := r.Find(level, msg, want...)
	if len(found) == 1 {
		return found[0]
	}
	t.Fatalf("logtest: want 1 entry %s %q with %v, found %d; captured:\n%s",
		level, msg, want, len(found), r.dump())
	return Entry{}
}

// AssertNotLogged fails t when any entry has msg, at any level.
func (r *Recorder) AssertNotLogged(t testing.TB, msg string) {
	t.Helper()
	for _, e := range r.Entries() {
		if e.Message == msg {
			t.Fatalf("logtest: want no entry %q, found: %s", msg, e.Format())
		}
	}
}

func (r *Recorder) dump() string {
	entries := r.Entries()
	if len(entries) == 0 {
		return "  (none)"
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = "  " + e.Format()
	}
	return strings.Join(lines, "\n")
}

func (r *Recorder) add(e Entry) {
	r.mu.Lock()
	r.entries = append(r.entries, e)
	r.mu.Unlock()
}

// captureHandler turns records into entries. It accepts every level.
type captureHandler struct {
	rec    *Recorder
	prefix string      // open groups, as "a.b."
	attrs  []slog.Attr // WithAttrs attributes, flattened
}

func (*captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := slices.Clone(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, h.prefix, []slog.Attr{a})
		return true
	})
	h.rec.add(Entry{Time: r.Time, Level: r.Level, Message: r.Message, Attrs: attrs, PC: r.PC})
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = flatten(slices.Clip(h.attrs), h.prefix, attrs)
	return &next
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.prefix = h.prefix + name + "."
	return &next
}

// flatten appends attrs to dst, resolved, with group members under dotted
// keys. It follows the slog.Handler rules: an attribute with an empty key and
// a non-group value is dropped, a group with an empty key is inlined, and an
// empty group is dropped.
func flatten(dst []slog.Attr, prefix string, attrs []slog.Attr) []slog.Attr {
	for _, a := range attrs {
		v := a.Value.Resolve()
		if v.Kind() == slog.KindGroup {
			groupPrefix := prefix
			if a.Key != "" {
				groupPrefix = prefix + a.Key + "."
			}
			dst = flatten(dst, groupPrefix, v.Group())
			continue
		}
		if a.Key == "" {
			continue
		}
		dst = append(dst, slog.Attr{Key: prefix + a.Key, Value: v})
	}
	return dst
}

func hasAll(e Entry, want []slog.Attr) bool {
	for _, w := range want {
		got, ok := e.Value(w.Key)
		if !ok || !valuesMatch(got, w.Value) {
			return false
		}
	}
	return true
}

func valuesMatch(got, want slog.Value) bool {
	if got.Kind() != want.Kind() {
		return false
	}
	if want.Kind() != slog.KindAny {
		return got.Equal(want)
	}
	if wantErr, ok := want.Any().(error); ok {
		gotErr, isErr := got.Any().(error)
		return isErr && errors.Is(gotErr, wantErr)
	}
	return reflect.DeepEqual(got.Any(), want.Any())
}

type recorderKey struct{}

// Capture returns a context derived from t.Context() whose package-level slog
// calls (slog.InfoContext(ctx, ...)) record into a new Recorder, and that
// Recorder.
//
// The first call installs a routing handler as the slog default. It sends each
// record to the Recorder on the record's context, so parallel tests each see
// only their own lines. A record whose context carries no Recorder, such as
// one from slog.Info without a context, goes to standard error as text. Do not
// call slog.SetDefault in a test package that uses Capture.
func Capture(t testing.TB) (context.Context, *Recorder) {
	t.Helper()
	installRouter()
	rec := newRecorder()
	return rec.Context(t.Context()), rec
}

// Context returns ctx carrying r, so package-level slog calls with the result
// record into r once Capture has installed the routing handler. Use it for a
// context Capture did not create, such as a request context in a handler test.
func (r *Recorder) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

var installOnce sync.Once //nolint:gochecknoglobals // the slog default is process-wide; install the router once.

func installRouter() {
	installOnce.Do(func() {
		// Not the previous default: the standard library's initial default
		// writes through the log package, which SetDefault redirects back to
		// slog, so routing to it would recurse.
		fallback := slog.NewTextHandler(os.Stderr, nil)
		slog.SetDefault(slog.New(&routerHandler{fallback: fallback}))
	})
}

// routerHandler sends a record to the Recorder on its context, replaying the
// logger's WithAttrs and WithGroup calls on that Recorder's handler, or to the
// fallback.
type routerHandler struct {
	fallback slog.Handler // with ops applied
	ops      []routerOp
}

type routerOp struct {
	group string
	attrs []slog.Attr
}

func (h *routerHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if _, ok := ctx.Value(recorderKey{}).(*Recorder); ok {
		return true
	}
	return h.fallback.Enabled(ctx, level)
}

func (h *routerHandler) Handle(ctx context.Context, r slog.Record) error {
	rec, ok := ctx.Value(recorderKey{}).(*Recorder)
	if !ok {
		return h.fallback.Handle(ctx, r)
	}
	handler := rec.handler
	for _, op := range h.ops {
		if op.group != "" {
			handler = handler.WithGroup(op.group)
		} else {
			handler = handler.WithAttrs(op.attrs)
		}
	}
	return handler.Handle(ctx, r)
}

func (h *routerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &routerHandler{
		fallback: h.fallback.WithAttrs(attrs),
		ops:      append(slices.Clip(h.ops), routerOp{attrs: attrs}),
	}
}

func (h *routerHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &routerHandler{
		fallback: h.fallback.WithGroup(name),
		ops:      append(slices.Clip(h.ops), routerOp{group: name}),
	}
}
