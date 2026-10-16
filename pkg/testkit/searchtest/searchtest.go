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

// Scope is one valid request the refusals are sent in.
type Scope struct {
	// Body must answer 200 as sent.
	Body map[string]any
	// Incompatible is a populated request whose ordering differs from Body's.
	// The kit takes the cursor it issues and sends it with Body, which must
	// refuse it. It works for empty scopes too, which issue no cursor of
	// their own. Nil skips the check.
	Incompatible map[string]any
	// SameOrder is a populated request with Body's ordering. An empty scope
	// issues no cursor, so the kit uses SameOrder's cursor to check that the
	// scope still refuses a valid cursor sent with an offset. A populated
	// scope uses its own cursor and needs none.
	SameOrder map[string]any
}

// Refusals describes the invalid requests a search must answer with 400.
type Refusals struct {
	// Scopes are the requests the refusals are sent in: at least one
	// populated scope, plus every scope where the search may short-circuit
	// (empty, no match, missing binding). A populated scope per sort field
	// lets Mutate forge every key type.
	Scopes []Scope
	// SortField is true when the endpoint accepts sort_by; the kit then sends
	// an unknown field and expects SortCode.
	SortField bool
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
			for _, err := range CheckRefusals(t.Context(), suite, scope) {
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
	var total int64
	for page := 0; ; page++ {
		if page >= suite.MaxPages {
			return fmt.Errorf("walk did not end within %d pages", suite.MaxPages)
		}
		p, err := fetchPage(ctx, suite, withPagination(c.Body, suite.Limit, cursor, 0))
		if err != nil {
			return fmt.Errorf("page %d: %w", page, err)
		}
		switch {
		case page > 0 && p.Total != total:
			return fmt.Errorf("page %d: total %d, first page said %d", page, p.Total, total)
		case len(p.IDs) == 0:
			// Want holds more rows than the limit, so every page of the walk
			// has rows: the first one does, and a keyset page reads one row
			// past the limit, so a cursor leading to an empty page was one it
			// should not have issued.
			return fmt.Errorf("page %d: empty page in a walk of %d rows", page, len(c.Want))
		}
		if page == 0 {
			total = p.Total
		}
		got = append(got, p.IDs...)
		if p.NextCursor == "" {
			break
		}
		if len(got) >= len(c.Want) {
			return fmt.Errorf("page %d: next_cursor after all %d expected rows", page, len(c.Want))
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

// validPage checks what every successful page must satisfy on its own. A
// first page (no cursor sent) must also hold rows whenever total says some
// match. Every page the kit reads goes through it.
func validPage(p Page, limit int, first bool) error {
	n := int64(len(p.IDs))
	switch {
	case p.Total < 0 || p.Count < 0:
		return fmt.Errorf("negative total %d or count %d", p.Total, p.Count)
	case p.Count != len(p.IDs):
		return fmt.Errorf("count %d, %d items", p.Count, len(p.IDs))
	case len(p.IDs) > limit:
		return fmt.Errorf("%d items exceed limit %d", len(p.IDs), limit)
	case n > p.Total:
		return fmt.Errorf("%d items exceed total %d", n, p.Total)
	case p.NextCursor != "" && (n == 0 || n >= p.Total):
		return fmt.Errorf("next_cursor on a page of %d items out of %d: no row can follow", n, p.Total)
	case first && p.Total > 0 && n == 0:
		return fmt.Errorf("first page is empty but total is %d", p.Total)
	case first && n < p.Total && p.NextCursor == "":
		return fmt.Errorf("first page holds %d of %d rows and issues no cursor: the rest is unreachable", n, p.Total)
	}
	return nil
}

// CheckRefusals sends the invalid requests in one scope and reports every
// violation. Forged cursors are tried in every scope that has a second page,
// so scopes with different sorts exercise different key types.
func CheckRefusals(ctx context.Context, suite Suite, s Scope) []error {
	scope := s.Body
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
	// A cursor this ordering accepts, sent with an offset, must be refused:
	// a malformed one would be refused for the wrong reason.
	offsetCursor := first.NextCursor
	if offsetCursor == "" && s.SameOrder != nil {
		same, err := fetchPage(ctx, suite, withPagination(s.SameOrder, suite.Limit, "", 0))
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("same-order request: %w", err))
		case same.NextCursor == "":
			errs = append(errs, fmt.Errorf("same-order request issued no cursor; seed more rows than the limit"))
		default:
			offsetCursor = same.NextCursor
		}
	}
	switch {
	case offsetCursor != "":
		refuse("cursor with offset", withPagination(scope, suite.Limit, offsetCursor, 1), cursorCode)
	case s.SameOrder == nil:
		errs = append(errs, fmt.Errorf("cursor with offset: the scope issued no cursor; give it SameOrder, a populated request with its ordering"))
	}
	if suite.Refusals.SortField {
		unknown := maps.Clone(scope)
		if unknown == nil {
			unknown = map[string]any{}
		}
		unknown["sort_by"] = "searchtest_unknown_field"
		refuse("unknown sort_by", withPagination(unknown, suite.Limit, "", 0), sortCode)
	}
	if s.Incompatible != nil {
		other, err := fetchPage(ctx, suite, withPagination(s.Incompatible, suite.Limit, "", 0))
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("incompatible request: %w", err))
		case other.NextCursor == "":
			errs = append(errs, fmt.Errorf("incompatible request issued no cursor; seed more rows than the limit"))
		default:
			refuse("cursor from another ordering", withPagination(scope, suite.Limit, other.NextCursor, 0), cursorCode)
		}
	}
	if first.NextCursor == "" {
		return errs
	}
	if suite.Mutate != nil {
		for i, forged := range suite.Mutate(first.NextCursor) {
			body := withPagination(scope, suite.Limit, forged, 0)
			status, response, err := suite.Call(ctx, body)
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("forged cursor %d: %w", i, err))
			case status == http.StatusOK:
				page, err := suite.Decode(response)
				if err == nil {
					err = validPage(page, suite.Limit, !hasCursor(body))
				}
				if err == nil && page.Total != first.Total {
					// Same filter, stable fixture: the count of matches cannot move.
					err = fmt.Errorf("total %d, the scope's total is %d", page.Total, first.Total)
				}
				if err != nil {
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
	if code != "" && !slices.Contains(errorCodes(response), code) {
		return fmt.Errorf("400 without error code %s: %s", code, truncate(response))
	}
	return nil
}

// errorCodes collects every "code" string in a JSON error body, at any depth,
// so the check reads the codes themselves rather than matching text that
// could sit in a message.
func errorCodes(response []byte) []string {
	var body any
	if err := json.Unmarshal(response, &body); err != nil {
		return nil
	}
	var codes []string
	var visit func(any)
	visit = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, value := range v {
				if code, ok := value.(string); ok && key == "code" {
					codes = append(codes, code)
				}
				visit(value)
			}
		case []any:
			for _, item := range v {
				visit(item)
			}
		}
	}
	visit(body)
	return codes
}

// fetchPage sends a request that must succeed and returns its page after
// validPage; a request without a cursor is checked as a first page.
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
	if err := validPage(p, suite.Limit, !hasCursor(body)); err != nil {
		return Page{}, fmt.Errorf("%w: %s", err, truncate(response))
	}
	return p, nil
}

func hasCursor(body map[string]any) bool {
	pagination, _ := body["pagination"].(map[string]any)
	cursor, _ := pagination["cursor"].(string)
	return cursor != ""
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
