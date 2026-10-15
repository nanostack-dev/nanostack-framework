package search

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	token, err := EncodeCursor("t.created_at:DESC,t.id:DESC", []any{"2026-10-15T10:00:00.123456Z", nil, int64(7)})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q is not URL-safe", token)
	}
	cursor, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if cursor.Order != "t.created_at:DESC,t.id:DESC" {
		t.Fatalf("Order = %q", cursor.Order)
	}
	got := []string{string(cursor.Values[0]), string(cursor.Values[1]), string(cursor.Values[2])}
	want := []string{`"2026-10-15T10:00:00.123456Z"`, "null", "7"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Values[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, token := range map[string]string{
		"empty":         "",
		"not base64":    "!!!",
		"not json":      encode("hello"),
		"wrong version": encode(`{"v":2,"o":"id:ASC","k":["a"]}`),
		"no order":      encode(`{"v":1,"o":"","k":["a"]}`),
		"no values":     encode(`{"v":1,"o":"id:ASC","k":[]}`),
		"too long":      strings.Repeat("a", MaxCursorLength+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCursor(token); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("DecodeCursor(%q) error = %v, want ErrInvalidCursor", token, err)
			}
		})
	}
}

func TestPaginationValidate(t *testing.T) {
	if err := (Pagination{Limit: 10, Offset: 20}).Validate(); err != nil {
		t.Fatalf("offset only: %v", err)
	}
	if err := (Pagination{Limit: 10, Cursor: "abc"}).Validate(); err != nil {
		t.Fatalf("cursor only: %v", err)
	}
	if err := (Pagination{Limit: 10, Offset: 1, Cursor: "abc"}).Validate(); !errors.Is(err, ErrCursorWithOffset) {
		t.Fatalf("cursor with offset: %v, want ErrCursorWithOffset", err)
	}
	long := Pagination{Limit: 10, Cursor: strings.Repeat("a", MaxCursorLength+1)}
	if err := long.Validate(); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("long cursor: %v, want ErrInvalidCursor", err)
	}
}

func TestWithCursorClearsOffset(t *testing.T) {
	req := NewRequest[struct{}, sortField]()
	req.Pagination.Offset = 40
	req = req.WithCursor("abc")
	if req.Pagination.Offset != 0 || req.Pagination.Cursor != "abc" {
		t.Fatalf("pagination = %+v", req.Pagination)
	}
	if one := req.One(); one.Pagination.Cursor != "" {
		t.Fatalf("One() kept the cursor: %+v", one.Pagination)
	}
}

func TestResultHasMore(t *testing.T) {
	if (Result[int]{}).HasMore() {
		t.Fatal("empty NextCursor reported more")
	}
	if !(Result[int]{NextCursor: "x"}).HasMore() {
		t.Fatal("NextCursor did not report more")
	}
}
