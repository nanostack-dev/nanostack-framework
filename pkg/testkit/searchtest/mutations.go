package searchtest

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// adversarialValues stand in for one ordering value of a real cursor. Each is
// valid JSON; whether the column can hold it depends on its type, so the
// search may answer 200 or 400, but never a server error.
var adversarialValues = []any{
	nil, "", "\x00", strings.Repeat("x", 2000), -1, 0, int64(math.MaxInt64), int64(math.MinInt64),
	1e308, -1e308, 0.5, "NaN", "Infinity", "-Infinity", true, map[string]any{}, []any{},
	"not-a-date", "2026-01-01T00:00:00+16:00", "00000000-0000-0000-0000-000000000000",
	"10.0.0.0/8", "::1", "'; DROP TABLE x; --",
}

// FrameworkCursorMutations returns forged variants of a cursor issued by this
// framework's keyset ordering: every value replaced by each adversarial
// value, values added and removed, another ordering signature, another
// version, and broken encodings. Use it only for cursors the search under
// test issues itself; a search that forwards another service's cursor
// cannot be expected to decode it.
func FrameworkCursorMutations(cursor string) []string {
	decoded, err := search.DecodeCursor(cursor)
	if err != nil {
		return nil
	}
	// Untouched values stay raw JSON: decoding them into any would turn a
	// large integer into a float64 and corrupt it, so every forgery aimed at
	// a later key would be refused for the wrong reason.
	values := make([]any, len(decoded.Values))
	for i, raw := range decoded.Values {
		values[i] = raw
	}
	var forged []string
	add := func(order string, vals []any) {
		if token, err := search.EncodeCursor(order, vals); err == nil {
			forged = append(forged, token)
		}
	}
	for i := range values {
		for _, adversarial := range adversarialValues {
			mutated := append([]any(nil), values...)
			mutated[i] = adversarial
			add(decoded.Order, mutated)
		}
	}
	add(decoded.Order, append(append([]any(nil), values...), "extra"))
	if len(values) > 1 {
		add(decoded.Order, values[:len(values)-1])
	}
	add(decoded.Order+",other.column:ASC", values)
	add("other.column:ASC", values)

	payload, _ := base64.RawURLEncoding.DecodeString(cursor)
	var wire map[string]any
	if json.Unmarshal(payload, &wire) == nil {
		wire["v"] = 99
		if b, err := json.Marshal(wire); err == nil {
			forged = append(forged, base64.RawURLEncoding.EncodeToString(b))
		}
	}
	forged = append(forged,
		cursor[:len(cursor)/2],
		base64.RawURLEncoding.EncodeToString([]byte("[]")),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"o":"","k":[]}`)),
		strings.ToUpper(cursor),
	)
	return forged
}
