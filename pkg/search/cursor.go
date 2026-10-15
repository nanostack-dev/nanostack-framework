package search

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// MaxCursorLength bounds the opaque cursor a client may send back. A cursor
// carries one value per ordering column, so a few hundred bytes is typical.
// The bound leaves room for several text sort values of 1,000 characters each,
// which is why a sortable text column must be bounded (VARCHAR(255) or a
// validated length): a longer value would issue a cursor the decoder refuses.
const MaxCursorLength = 8192

const cursorVersion = 1

var (
	// ErrInvalidCursor reports a cursor that cannot continue this search: it
	// is malformed, too long, or was issued for a different ordering. Callers
	// map it to a client error (HTTP 400).
	ErrInvalidCursor = errors.New("search: invalid cursor")

	// ErrCursorWithOffset reports a request that sets both a cursor and a
	// non-zero offset. A cursor already says where the page starts, so the two
	// cannot be combined. Callers map it to a client error (HTTP 400).
	ErrCursorWithOffset = errors.New("search: cursor cannot be combined with offset")
)

// Cursor is the decoded form of an opaque pagination token: the ordering it
// was issued for and the last row's value for each ordering column.
//
// A cursor is position, not authority. It is neither signed nor encrypted:
// tenant scope and filters always come from the request, so an edited cursor
// can at most move the start of a page inside rows the caller may already
// read. Never put anything into a cursor that the caller may not see.
type Cursor struct {
	// Order identifies the ordering, for example "created_at:DESC,id:DESC".
	// A cursor only continues the ordering it was issued for.
	Order string
	// Values holds one JSON value per ordering column, in order. A JSON null
	// is a SQL NULL.
	Values []json.RawMessage
}

type cursorWire struct {
	Version int               `json:"v"`
	Order   string            `json:"o"`
	Values  []json.RawMessage `json:"k"`
}

// EncodeCursor builds the opaque token for the row whose ordering values are
// given. Values must be JSON-encodable; nil encodes a SQL NULL.
func EncodeCursor(order string, values []any) (string, error) {
	raw := make([]json.RawMessage, len(values))
	for i, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("search: encode cursor value %d: %w", i, err)
		}
		raw[i] = encoded
	}
	payload, err := json.Marshal(cursorWire{Version: cursorVersion, Order: order, Values: raw})
	if err != nil {
		return "", fmt.Errorf("search: encode cursor: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(payload)
	if len(token) > MaxCursorLength {
		// Issuing it would strand the client: DecodeCursor refuses it. The
		// fix is a bounded sort column, so fail where the cause is visible.
		return "", fmt.Errorf("search: cursor for %q is %d bytes, over the %d limit; sort only on bounded columns",
			order, len(token), MaxCursorLength)
	}
	return token, nil
}

// DecodeCursor parses a token produced by EncodeCursor. Every failure wraps
// ErrInvalidCursor, so a caller translates them with one errors.Is.
func DecodeCursor(token string) (Cursor, error) {
	if token == "" || len(token) > MaxCursorLength {
		return Cursor{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: not base64url", ErrInvalidCursor)
	}
	var wire cursorWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Cursor{}, fmt.Errorf("%w: not a cursor payload", ErrInvalidCursor)
	}
	if wire.Version != cursorVersion || wire.Order == "" || len(wire.Values) == 0 {
		return Cursor{}, fmt.Errorf("%w: unsupported cursor", ErrInvalidCursor)
	}
	return Cursor{Order: wire.Order, Values: wire.Values}, nil
}

// Validate reports whether the pagination can run: a cursor must fit
// MaxCursorLength and cannot be combined with a non-zero offset. The limit and
// offset bounds stay with the struct tags.
func (p Pagination) Validate() error {
	if p.Cursor == "" {
		return nil
	}
	if p.Offset > 0 {
		return ErrCursorWithOffset
	}
	if len(p.Cursor) > MaxCursorLength {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidCursor, MaxCursorLength)
	}
	return nil
}
