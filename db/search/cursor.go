package search

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const cursorVersion = 1

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
// given. Values must be JSON-encodable; nil encodes a SQL NULL. A token longer
// than MaxCursorLength is an error of the search, not of the client: sort only
// on bounded columns.
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

// DecodeCursor parses a token that EncodeCursor produced. Every failure wraps
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
	if json.Unmarshal(payload, &wire) != nil {
		return Cursor{}, fmt.Errorf("%w: not a cursor payload", ErrInvalidCursor)
	}
	if wire.Version != cursorVersion || wire.Order == "" || len(wire.Values) == 0 {
		return Cursor{}, fmt.Errorf("%w: unsupported cursor", ErrInvalidCursor)
	}
	return Cursor{Order: wire.Order, Values: wire.Values}, nil
}
