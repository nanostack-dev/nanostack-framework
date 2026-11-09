package search_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// The golden tokens were issued by pkg/search before the move (nanostack-framework
// 64955dc). A cursor that a client holds must continue after the move, so the
// wire format cannot change.
const (
	goldenTimeOrder = "eyJ2IjoxLCJvIjoidC5jcmVhdGVkX2F0OkRFU0MsdC5pZDpERVNDIiwiayI6WyIyMDI2LTEwLTE1VDEwOjAwOjAwLjEy" +
		"MzQ1NloiLG51bGwsN119"
	goldenMixedValues = "eyJ2IjoxLCJvIjoibmFtZTpBU0MsaWQ6QVNDIiwiayI6WyLDnG7Dr2PDuGTDqSBcdTAwM2NcdTAwMjZcdTAwM2Ui" +
		"LDMuNSx0cnVlLC00Mix7ImEiOjF9XX0"
)

func TestEncodeCursorGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		order  string
		values []any
		want   string
	}{
		{"time order with a NULL", "t.created_at:DESC,t.id:DESC",
			[]any{"2026-10-15T10:00:00.123456Z", nil, int64(7)}, goldenTimeOrder},
		{"mixed values, HTML escaped", "name:ASC,id:ASC",
			[]any{"Ünïcødé <&>", 3.5, true, int64(-42), map[string]any{"a": 1}}, goldenMixedValues},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := search.EncodeCursor(tt.order, tt.values)
			if err != nil {
				t.Fatalf("EncodeCursor() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EncodeCursor() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	token, err := search.EncodeCursor("t.created_at:DESC,t.id:DESC",
		[]any{"2026-10-15T10:00:00.123456Z", nil, int64(7)})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q is not URL-safe", token)
	}
	cursor, err := search.DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if cursor.Order != "t.created_at:DESC,t.id:DESC" {
		t.Fatalf("Order = %q", cursor.Order)
	}
	want := []string{`"2026-10-15T10:00:00.123456Z"`, "null", "7"}
	if len(cursor.Values) != len(want) {
		t.Fatalf("Values = %s, want %d values", cursor.Values, len(want))
	}
	for i := range want {
		if string(cursor.Values[i]) != want[i] {
			t.Fatalf("Values[%d] = %s, want %s", i, cursor.Values[i], want[i])
		}
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	t.Parallel()
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, token := range map[string]string{
		"empty":         "",
		"not base64":    "!!!",
		"padded base64": base64.URLEncoding.EncodeToString([]byte(`{"v":1,"o":"id:ASC","k":["ab"]}`)),
		"not json":      encode("hello"),
		"wrong version": encode(`{"v":2,"o":"id:ASC","k":["a"]}`),
		"no version":    encode(`{"o":"id:ASC","k":["a"]}`),
		"no order":      encode(`{"v":1,"o":"","k":["a"]}`),
		"no values":     encode(`{"v":1,"o":"id:ASC","k":[]}`),
		"too long":      strings.Repeat("a", search.MaxCursorLength+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := search.DecodeCursor(token)
			if !errors.Is(err, search.ErrInvalidCursor) {
				t.Fatalf("DecodeCursor(%q) error = %v, want ErrInvalidCursor", token, err)
			}
			if !search.IsClientError(err) {
				t.Fatalf("DecodeCursor(%q) error = %v, which is not a client error", token, err)
			}
		})
	}
}

func TestEncodeCursorRefusesOversizedTokens(t *testing.T) {
	t.Parallel()
	_, err := search.EncodeCursor("t.name:ASC,t.id:ASC", []any{strings.Repeat("x", search.MaxCursorLength), "id"})
	if err == nil {
		t.Fatal("EncodeCursor() issued a cursor that DecodeCursor refuses")
	}
	// An oversized cursor is a fault of the search (an unbounded sort column),
	// not of the client.
	if search.IsClientError(err) {
		t.Fatalf("EncodeCursor() error %v is a client error", err)
	}
	token, err := search.EncodeCursor("t.name:ASC,t.id:ASC",
		[]any{strings.Repeat("é", 1000), strings.Repeat("x", 1000)})
	if err != nil {
		t.Fatalf("two 1,000-character values: %v", err)
	}
	if _, decodeErr := search.DecodeCursor(token); decodeErr != nil {
		t.Fatalf("DecodeCursor() of an issued cursor: %v", decodeErr)
	}
}

func TestEncodeCursorRefusesValuesThatAreNotJSON(t *testing.T) {
	t.Parallel()
	if _, err := search.EncodeCursor("id:ASC", []any{make(chan int)}); err == nil {
		t.Fatal("EncodeCursor() encoded a channel")
	}
}
