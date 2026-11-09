package search_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// TestCursorLengthBoundary pins MaxCursorLength on both sides. A token of
// exactly MaxCursorLength bytes is issued, decoded and accepted by CheckPage.
// One more value byte is refused by EncodeCursor. A well-formed token over the
// bound fails at the length check, not at base64.
func TestCursorLengthBoundary(t *testing.T) {
	t.Parallel()
	// The payload {"v":1,"o":"id:ASC","k":["x…x"]} of 6144 bytes is 8192
	// base64url characters.
	const overhead = len(`{"v":1,"o":"id:ASC","k":[""]}`)
	value := strings.Repeat("x", search.MaxCursorLength/4*3-overhead)
	token, err := search.EncodeCursor("id:ASC", []any{value})
	if err != nil || len(token) != search.MaxCursorLength {
		t.Fatalf("EncodeCursor() = %d bytes, %v; want %d bytes, nil", len(token), err, search.MaxCursorLength)
	}
	if _, decodeErr := search.DecodeCursor(token); decodeErr != nil {
		t.Fatalf("DecodeCursor(issued %d-byte token) = %v", len(token), decodeErr)
	}
	if pageErr := search.CheckPage(search.Pagination{Limit: 1, Cursor: token}); pageErr != nil {
		t.Fatalf("CheckPage(%d-byte cursor) = %v", len(token), pageErr)
	}
	if _, overErr := search.EncodeCursor("id:ASC", []any{value + "x"}); overErr == nil {
		t.Fatal("EncodeCursor() issued a token over MaxCursorLength")
	}
	long := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"o":"id:ASC","k":["` + value + `xxx"]}`))
	if _, longErr := search.DecodeCursor(long); !errors.Is(longErr, search.ErrInvalidCursor) {
		t.Fatalf("DecodeCursor(%d-byte well-formed token) = %v, want ErrInvalidCursor", len(long), longErr)
	}
}
