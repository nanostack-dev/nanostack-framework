package log

import (
	"context"
	"encoding"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// Credentials are held as secret.Secret (package core/secret), which every
// encoder prints as secret.Redacted. This file adds the second control: the
// handler replaces the value of every attribute whose key secret.IsKey matches
// with the same marker.
//
// Types that hold secrets among other data follow the same convention:
// implement slog.LogValuer and return only the fields that are safe to log.
//
//	func (k APIKey) LogValue() slog.Value {
//		return slog.GroupValue(slog.String("id", k.ID), slog.String("name", k.Name))
//	}

// redactHandler replaces the value of every attribute whose key secret.IsKey
// matches,
// on records and on attributes added with WithAttrs. It descends into groups,
// into LogValuers that resolve to groups, and into maps with string keys
// (http.Header, url.Values, map[string]string, map[string]any, ...), which it
// turns into groups so the same key guard applies to their entries.
//
// It resolves every LogValuer once and hands the resolved values on, so the
// output handler does not call LogValue a second time.
type redactHandler struct {
	inner slog.Handler
}

func newRedactHandler(inner slog.Handler) *redactHandler {
	return &redactHandler{inner: inner}
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	dirty := false
	r.Attrs(func(a slog.Attr) bool {
		dirty = mayChange(a)
		return !dirty
	})
	if !dirty {
		return h.inner.Handle(ctx, r)
	}
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(clean(a, 0))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cleaned := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		cleaned[i] = clean(a, 0)
	}
	return &redactHandler{inner: h.inner.WithAttrs(cleaned)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{inner: h.inner.WithGroup(name)}
}

// mayChange reports whether clean could return something other than a. It
// never resolves a LogValuer, so the record's valuers run once, in clean.
func mayChange(a slog.Attr) bool {
	if secret.IsKey(a.Key) {
		return true
	}
	switch a.Value.Kind() {
	case slog.KindLogValuer:
		return true
	case slog.KindGroup:
		return slices.ContainsFunc(a.Value.Group(), mayChange)
	case slog.KindAny:
		return isStringKeyedMap(a.Value.Any())
	default:
		return false
	}
}

// maxDepth bounds how deep clean descends into nested maps, which can form a
// cycle. A value below it is replaced by truncated.
const maxDepth = 16

const truncated = "[TRUNCATED]"

// clean returns a with its value resolved, replaced by secret.Redacted when the key is
// secret, and cleaned recursively when it is a group or a map with string keys.
func clean(a slog.Attr, depth int) slog.Attr {
	if secret.IsKey(a.Key) {
		return slog.String(a.Key, secret.Redacted)
	}
	if depth > maxDepth {
		return slog.String(a.Key, truncated)
	}
	value := a.Value.Resolve()
	switch value.Kind() {
	case slog.KindGroup:
		members := value.Group()
		cleaned := make([]slog.Attr, len(members))
		for i, member := range members {
			cleaned[i] = clean(member, depth+1)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(cleaned...)}
	case slog.KindAny:
		if group, ok := mapGroup(value.Any(), depth); ok {
			return slog.Attr{Key: a.Key, Value: group}
		}
	default:
	}
	return slog.Attr{Key: a.Key, Value: value}
}

// mapGroup returns m as a group of its entries, sorted by key and cleaned, when
// m is a non-empty map with string keys. A map type with its own JSON or text
// encoding keeps it.
func mapGroup(m any, depth int) (slog.Value, bool) {
	switch m := m.(type) {
	case http.Header:
		return groupOf(m, depth)
	case url.Values:
		return groupOf(m, depth)
	case map[string][]string:
		return groupOf(m, depth)
	case map[string]string:
		return groupOf(m, depth)
	case map[string]any:
		return groupOf(m, depth)
	}
	if !isStringKeyedMap(m) {
		return slog.Value{}, false
	}
	rv := reflect.ValueOf(m)
	if rv.Len() == 0 {
		return slog.Value{}, false
	}
	keys := rv.MapKeys()
	slices.SortFunc(keys, func(x, y reflect.Value) int { return strings.Compare(x.String(), y.String()) })
	attrs := make([]slog.Attr, len(keys))
	for i, key := range keys {
		attrs[i] = clean(slog.Any(key.String(), rv.MapIndex(key).Interface()), depth+1)
	}
	return slog.GroupValue(attrs...), true
}

func groupOf[M ~map[string]V, V any](m M, depth int) (slog.Value, bool) {
	if len(m) == 0 {
		return slog.Value{}, false
	}
	attrs := make([]slog.Attr, 0, len(m))
	for _, key := range slices.Sorted(maps.Keys(m)) {
		attrs = append(attrs, clean(slog.Any(key, m[key]), depth+1))
	}
	return slog.GroupValue(attrs...), true
}

// isStringKeyedMap reports whether v is a map whose keys are strings and whose
// type does not encode itself. Reflection runs only on values slog would hand
// to encoding/json or fmt anyway.
func isStringKeyedMap(v any) bool {
	switch v.(type) {
	case http.Header, url.Values, map[string][]string, map[string]string, map[string]any:
		return true
	case nil, error, json.Marshaler, encoding.TextMarshaler:
		return false
	}
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Map && t.Key().Kind() == reflect.String
}
