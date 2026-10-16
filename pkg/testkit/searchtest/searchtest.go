// Package searchtest checks a cursor-paginated search through its HTTP API.
//
// It verifies the contract every search shares, against data the caller has
// seeded and made stable: a cursor walk returns exactly the expected rows in
// the expected order, pages respect the limit, totals hold, the walk ends,
// and bad input answers 400 with the documented error code in every scope,
// including scopes where the search could short-circuit to an empty page.
//
// The kit owns no fixtures and knows no endpoint. The suite supplies a Call
// that sends one request body, a Decode that reads one page from a response,
// and explicit cases with their expected order. Cursor internals are optional:
// a search that issues framework cursors adds FrameworkCursorMutations; a
// search that forwards another service's cursor does not.
package searchtest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// Call sends one search request body and returns the HTTP status and body.
type Call func(ctx context.Context, body map[string]any) (status int, response []byte, err error)

// Page is one decoded search page.
type Page struct {
	IDs        []string
	Total      int64
	Count      int
	NextCursor string
}

// Decoder reads one page from a successful response body. It must fail on a
// body missing a required field rather than return zero values.
type Decoder func(response []byte) (Page, error)

// Case is one walk: a request body (filter, sort_by, sort_direction as the
// endpoint accepts them; omit a key to test its default) and the ids the
// search must return, in order, across the whole walk.
type Case struct {
	Name string
	Body map[string]any
	Want []string
}

// Refusals describes the invalid requests a search must answer with 400.
type Refusals struct {
	// Scopes are valid request bodies the refusals are sent in: at least one
	// populated scope, plus every scope where the search may short-circuit
	// (empty, no match, missing binding). Each must answer 200 as sent. A
	// populated scope per sort field lets Mutate forge every key type.
	Scopes []map[string]any
	// SortField is true when the endpoint accepts sort_by; the kit then sends
	// an unknown field and expects SortCode.
	SortField bool
	// Incompatible is a body whose ordering differs from Scopes[0]'s; a cursor
	// from Scopes[0] sent with it must be refused. Nil skips the check.
	Incompatible map[string]any
	// CursorCode and SortCode are the error codes the 400 must carry; they
	// default to INVALID_CURSOR and INVALID_SORT. A service that refuses an
	// unknown sort in request validation sets SortCode to that code.
	CursorCode string
	SortCode   string
}

// Suite is everything the kit needs for one search.
type Suite struct {
	Name     string
	Call     Call
	Decode   Decoder
	Limit    int // page size of every walk; default 2
	MaxPages int // bound on requests per walk; default 200
	Cases    []Case
	Refusals Refusals
	// Mutate returns forged variants of a cursor the search issued. Every
	// variant must answer 200 with a valid page or 400, never anything else.
	// Use FrameworkCursorMutations for cursors issued by this framework.
	Mutate func(cursor string) []string
}

// Run executes every check as subtests of t.
func Run(t *testing.T, suite Suite) {
	t.Helper()
	if suite.Call == nil {
		t.Fatal("searchtest: Suite.Call is required")
	}
	suite = withDefaults(suite)
	for _, c := range suite.Cases {
		t.Run(suite.Name+"/walk/"+c.Name, func(t *testing.T) {
			if err := CheckWalk(t.Context(), suite, c); err != nil {
				t.Fatal(err)
			}
		})
	}
	for i, scope := range suite.Refusals.Scopes {
		t.Run(fmt.Sprintf("%s/refusals/scope-%d", suite.Name, i), func(t *testing.T) {
			for _, err := range CheckRefusals(t.Context(), suite, scope, i == 0) {
				t.Error(err)
			}
		})
	}
}

func withDefaults(suite Suite) Suite {
	if suite.Decode == nil {
		suite.Decode = DecodeItems("items", "id")
	}
	if suite.Limit <= 0 {
		suite.Limit = 2
	}
	if suite.MaxPages <= 0 {
		suite.MaxPages = 200
	}
	return suite
}

// CheckWalk walks one case by cursor and reports the first contract
// violation, or nil.
func CheckWalk(ctx context.Context, suite Suite, c Case) error {
	suite = withDefaults(suite)
	if len(c.Want) <= suite.Limit {
		return fmt.Errorf("case %q expects %d rows; seed more than the limit (%d) so the walk crosses a page",
			c.Name, len(c.Want), suite.Limit)
	}
	var got []string
	seenCursors := map[string]bool{}
	cursor := ""
	total := int64(-1)
	for page := 0; ; page++ {
		if page >= suite.MaxPages {
			return fmt.Errorf("walk did not end within %d pages", suite.MaxPages)
		}
		p, err := fetchPage(ctx, suite, withPagination(c.Body, suite.Limit, cursor, 0))
		if err != nil {
			return fmt.Errorf("page %d: %w", page, err)
		}
		switch {
		case p.Count != len(p.IDs):
			return fmt.Errorf("page %d: count %d, %d items", page, p.Count, len(p.IDs))
		case len(p.IDs) > suite.Limit:
			return fmt.Errorf("page %d: %d items exceed limit %d", page, len(p.IDs), suite.Limit)
		case total >= 0 && p.Total != total:
			return fmt.Errorf("page %d: total %d, first page said %d", page, p.Total, total)
		}
		total = p.Total
		got = append(got, p.IDs...)
		if p.NextCursor == "" {
			break
		}
		if len(p.IDs) == 0 {
			return fmt.Errorf("page %d: empty page with a next_cursor", page)
		}
		if seenCursors[p.NextCursor] {
			return fmt.Errorf("page %d: next_cursor repeats an earlier one; the walk would never end", page)
		}
		seenCursors[p.NextCursor] = true
		cursor = p.NextCursor
	}
	if !slices.Equal(got, c.Want) {
		return fmt.Errorf("walk returned %v, want %v", got, c.Want)
	}
	if total != int64(len(c.Want)) {
		return fmt.Errorf("total %d, walk returned %d rows", total, len(c.Want))
	}
	return nil
}

// CheckRefusals sends the invalid requests in one scope and reports every
// violation. primary marks Refusals.Scopes[0], the scope Incompatible is
// defined against; forged cursors are tried in every scope that has a second
// page, so scopes with different sorts exercise different key types.
func CheckRefusals(ctx context.Context, suite Suite, scope map[string]any, primary bool) []error {
	suite = withDefaults(suite)
	cursorCode := orDefault(suite.Refusals.CursorCode, "INVALID_CURSOR")
	sortCode := orDefault(suite.Refusals.SortCode, "INVALID_SORT")
	var errs []error
	refuse := func(name string, body map[string]any, code string) {
		if err := expectRefusal(ctx, suite, body, code); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	// Control: the scope as sent is valid, so a 400 below is about the change.
	first, err := fetchPage(ctx, suite, withPagination(scope, suite.Limit, "", 0))
	if err != nil {
		return []error{fmt.Errorf("control request: %w", err)}
	}
	refuse("malformed cursor", withPagination(scope, suite.Limit, "!!not-a-cursor!!", 0), cursorCode)
	refuse("oversized cursor", withPagination(scope, suite.Limit, strings.Repeat("A", search.MaxCursorLength+1), 0), cursorCode)
	// Any non-empty cursor with an offset is refused before it is decoded.
	offsetCursor := first.NextCursor
	if offsetCursor == "" {
		offsetCursor = "c3RhbGU"
	}
	refuse("cursor with offset", withPagination(scope, suite.Limit, offsetCursor, 1), cursorCode)
	if suite.Refusals.SortField {
		unknown := maps.Clone(scope)
		if unknown == nil {
			unknown = map[string]any{}
		}
		unknown["sort_by"] = "searchtest_unknown_field"
		refuse("unknown sort_by", withPagination(unknown, suite.Limit, "", 0), sortCode)
	}
	if first.NextCursor == "" {
		return errs
	}
	if primary && suite.Refusals.Incompatible != nil {
		refuse("cursor from another ordering",
			withPagination(suite.Refusals.Incompatible, suite.Limit, first.NextCursor, 0), cursorCode)
	}
	if suite.Mutate != nil {
		for i, forged := range suite.Mutate(first.NextCursor) {
			status, response, err := suite.Call(ctx, withPagination(scope, suite.Limit, forged, 0))
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("forged cursor %d: %w", i, err))
			case status == http.StatusOK:
				if _, err := suite.Decode(response); err != nil {
					errs = append(errs, fmt.Errorf("forged cursor %d: 200 with an invalid page: %w", i, err))
				}
			case status != http.StatusBadRequest:
				errs = append(errs, fmt.Errorf("forged cursor %d %q: status %d, want 200 or 400: %s", i, forged, status, truncate(response)))
			}
		}
	}
	return errs
}

func expectRefusal(ctx context.Context, suite Suite, body map[string]any, code string) error {
	status, response, err := suite.Call(ctx, body)
	if err != nil {
		return err
	}
	if status != http.StatusBadRequest {
		return fmt.Errorf("status %d, want 400: %s", status, truncate(response))
	}
	if code != "" && !strings.Contains(string(response), `"`+code+`"`) {
		return fmt.Errorf("400 without error code %s: %s", code, truncate(response))
	}
	return nil
}

func fetchPage(ctx context.Context, suite Suite, body map[string]any) (Page, error) {
	status, response, err := suite.Call(ctx, body)
	if err != nil {
		return Page{}, err
	}
	if status != http.StatusOK {
		return Page{}, fmt.Errorf("status %d, want 200: %s", status, truncate(response))
	}
	p, err := suite.Decode(response)
	if err != nil {
		return Page{}, fmt.Errorf("decode page: %w: %s", err, truncate(response))
	}
	return p, nil
}

func withPagination(body map[string]any, limit int, cursor string, offset int) map[string]any {
	out := maps.Clone(body)
	if out == nil {
		out = map[string]any{}
	}
	pagination := map[string]any{"limit": limit}
	if cursor != "" {
		pagination["cursor"] = cursor
	}
	if offset != 0 {
		pagination["offset"] = offset
	}
	out["pagination"] = pagination
	return out
}

// DecodeItems decodes the common page shape: an items array whose elements
// carry idField, an integer total and count, and an optional next_cursor.
// A missing or mistyped field is an error.
func DecodeItems(itemsField, idField string) Decoder {
	return func(response []byte) (Page, error) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(response, &raw); err != nil {
			return Page{}, fmt.Errorf("not a JSON object: %w", err)
		}
		var page Page
		var items []map[string]json.RawMessage
		if err := requireField(raw, itemsField, &items); err != nil {
			return Page{}, err
		}
		if err := requireField(raw, "total", &page.Total); err != nil {
			return Page{}, err
		}
		if err := requireField(raw, "count", &page.Count); err != nil {
			return Page{}, err
		}
		if next, ok := raw["next_cursor"]; ok && string(next) != "null" {
			if err := json.Unmarshal(next, &page.NextCursor); err != nil {
				return Page{}, fmt.Errorf("next_cursor: %w", err)
			}
		}
		for i, item := range items {
			var id string
			if err := requireField(item, idField, &id); err != nil {
				return Page{}, fmt.Errorf("item %d: %w", i, err)
			}
			page.IDs = append(page.IDs, id)
		}
		return page, nil
	}
}

func requireField[V any](raw map[string]json.RawMessage, name string, into *V) error {
	value, ok := raw[name]
	if !ok || string(value) == "null" {
		return fmt.Errorf("missing field %q", name)
	}
	if err := json.Unmarshal(value, into); err != nil {
		return fmt.Errorf("field %q: %w", name, err)
	}
	return nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func truncate(body []byte) string {
	const limit = 500
	if len(body) > limit {
		return string(body[:limit]) + "…"
	}
	return string(body)
}
