package searchtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// fakeSearch is an in-memory search over rows with ids and names. Its defect
// flags each break one part of the contract, so the tests can show the kit
// catches it.
type fakeSearch struct {
	rows []fakeRow

	ignoreSort         bool // always orders by id
	stopAfterFirst     bool // first page claims to be the last
	repeatCursor       bool // next_cursor never advances
	serverErrorForged  bool // 500 on a cursor whose values do not decode
	acceptUnknownSort  bool // falls back to the default order
	wrongCount         bool // count is off by one
	shortcutEmpty      bool // returns an empty page before validating input in the empty scope
	emptySkipsOrder    bool // empty scope decodes the cursor but never checks its ordering
	codeInMessage      bool // puts the expected code in the message, not the code field
	extraCursorAtEnd   bool // issues a cursor on the last populated page
	negativeFirstTotal bool // first page reports total -1
	emptyFirstPage     bool // first page has no rows but a cursor
	emptyIgnoresOffset bool // empty scope accepts a valid cursor with an offset
	forgedBadPage      bool // answers a forged cursor with an inconsistent 200
	forgedZeroTotal    bool // answers a forged cursor with an empty page and total 0
	cursorOnLastPage   bool // the first page of a one-page result carries a cursor
}

type fakeRow struct{ ID, Name string }

func (f fakeSearch) call(_ context.Context, body map[string]any) (int, []byte, error) {
	encoded, _ := json.Marshal(body)
	var req struct {
		Scope      string `json:"scope"`
		SortBy     string `json:"sort_by"`
		Pagination struct {
			Limit  int    `json:"limit"`
			Offset int    `json:"offset"`
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(encoded, &req); err != nil {
		return 0, nil, err
	}
	rows := f.rows
	if req.Scope == "empty" {
		if f.shortcutEmpty {
			return ok(nil, 0, "")
		}
		rows = nil
	}
	sortBy := req.SortBy
	switch sortBy {
	case "", "id", "name":
	default:
		if !f.acceptUnknownSort {
			return f.refuse("INVALID_SORT")
		}
		sortBy = ""
	}
	if f.ignoreSort {
		sortBy = ""
	}
	offsetChecked := !(f.emptyIgnoresOffset && req.Scope == "empty")
	if err := (search.Pagination{Limit: 1, Offset: int32(req.Pagination.Offset), Cursor: req.Pagination.Cursor}).Validate(); err != nil && offsetChecked {
		return f.refuse("INVALID_CURSOR")
	}
	order := "id:ASC"
	key := func(r fakeRow) string { return r.ID }
	if sortBy == "name" {
		order = "name:ASC,id:ASC"
		key = func(r fakeRow) string { return r.Name + "\x00" + r.ID }
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b fakeRow) int { return strings.Compare(key(a), key(b)) })

	start := 0
	if req.Pagination.Cursor != "" {
		cursor, err := search.DecodeCursor(req.Pagination.Cursor)
		if err != nil {
			return f.refuse("INVALID_CURSOR")
		}
		if cursor.Order != order && !(f.emptySkipsOrder && req.Scope == "empty") {
			return f.refuse("INVALID_CURSOR")
		}
		var after string
		if err := json.Unmarshal(cursor.Values[0], &after); err != nil {
			if f.forgedBadPage {
				return http.StatusOK, []byte(`{"items":[],"total":-1,"count":42}`), nil
			}
			if f.forgedZeroTotal {
				return http.StatusOK, []byte(`{"items":[],"total":0,"count":0}`), nil
			}
			if f.serverErrorForged {
				return http.StatusInternalServerError, []byte(`{"errors":[{"code":"UNEXPECTED_ERROR"}]}`), nil
			}
			return f.refuse("INVALID_CURSOR")
		}
		for start < len(sorted) && key(sorted[start]) <= after {
			start++
		}
	}
	if f.emptyFirstPage && req.Pagination.Cursor == "" && req.Scope != "empty" {
		first, _ := search.EncodeCursor(order, []any{""})
		return ok(nil, int64(len(sorted)), first)
	}
	end := min(start+req.Pagination.Limit, len(sorted))
	page := sorted[start:end]
	next := ""
	if (end < len(sorted) || (f.extraCursorAtEnd && len(page) > 0) || (f.cursorOnLastPage && len(page) > 0 && start == 0)) && !f.stopAfterFirst {
		last := key(page[len(page)-1])
		if f.repeatCursor {
			last = key(sorted[0])
		}
		next, _ = search.EncodeCursor(order, []any{last})
	}
	count := len(page)
	if f.wrongCount {
		count++
	}
	total := int64(len(sorted))
	if f.negativeFirstTotal && req.Pagination.Cursor == "" {
		total = -1
	}
	return ok(page, total, next, count)
}

func ok(rows []fakeRow, total int64, next string, count ...int) (int, []byte, error) {
	items := make([]map[string]string, len(rows))
	for i, r := range rows {
		items[i] = map[string]string{"id": r.ID, "name": r.Name}
	}
	n := len(rows)
	if len(count) > 0 {
		n = count[0]
	}
	body := map[string]any{"items": items, "total": total, "count": n}
	if next != "" {
		body["next_cursor"] = next
	}
	encoded, err := json.Marshal(body)
	return http.StatusOK, encoded, err
}

func (f fakeSearch) refuse(code string) (int, []byte, error) {
	if f.codeInMessage {
		return http.StatusBadRequest, []byte(fmt.Sprintf(`{"errors":[{"code":"WRONG","message":%q}]}`, code)), nil
	}
	return http.StatusBadRequest, []byte(fmt.Sprintf(`{"errors":[{"code":%q}]}`, code)), nil
}

// Rows share names in pairs, so ordering by name needs the id tiebreak and
// differs from ordering by id.
var fakeRows = []fakeRow{
	{ID: "r1", Name: "b"}, {ID: "r2", Name: "a"}, {ID: "r3", Name: "b"},
	{ID: "r4", Name: "a"}, {ID: "r5", Name: "c"},
}

func fakeSuite(f fakeSearch) Suite {
	f.rows = fakeRows
	return Suite{
		Name: "fake",
		Call: f.call,
		Cases: []Case{
			{Name: "default", Body: map[string]any{}, Want: []string{"r1", "r2", "r3", "r4", "r5"}},
			{Name: "by name", Body: map[string]any{"sort_by": "name"}, Want: []string{"r2", "r4", "r1", "r3", "r5"}},
		},
		Refusals: Refusals{
			Scopes: []Scope{
				{Body: map[string]any{}, Incompatible: map[string]any{"sort_by": "name"}},
				{Body: map[string]any{"scope": "empty"}, Incompatible: map[string]any{"sort_by": "name"}, SameOrder: map[string]any{}},
			},
			SortField: true,
		},
		Mutate: FrameworkCursorMutations,
	}
}

func checkAll(suite Suite) error {
	suite = withDefaults(suite)
	var errs []error
	for _, c := range suite.Cases {
		if err := CheckWalk(context.Background(), suite, c); err != nil {
			errs = append(errs, fmt.Errorf("walk %s: %w", c.Name, err))
		}
	}
	for _, scope := range suite.Refusals.Scopes {
		errs = append(errs, CheckRefusals(context.Background(), suite, scope)...)
	}
	return errors.Join(errs...)
}

func TestRunPassesACorrectSearch(t *testing.T) {
	Run(t, fakeSuite(fakeSearch{}))
}

func TestChecksCatchEachDefect(t *testing.T) {
	for name, tc := range map[string]struct {
		search fakeSearch
		want   string
	}{
		"sort ignored":                   {fakeSearch{ignoreSort: true}, "walk by name"},
		"walk truncated":                 {fakeSearch{stopAfterFirst: true}, "walk default"},
		"cursor never advances":          {fakeSearch{repeatCursor: true}, "repeats an earlier one"},
		"server error on forged cursor":  {fakeSearch{serverErrorForged: true}, "status 500"},
		"unknown sort silently accepted": {fakeSearch{acceptUnknownSort: true}, "unknown sort_by"},
		"count disagrees with items":     {fakeSearch{wrongCount: true}, "count"},
		"shortcut skips validation":      {fakeSearch{shortcutEmpty: true}, "malformed cursor"},
		"empty scope ignores ordering":   {fakeSearch{emptySkipsOrder: true}, "cursor from another ordering"},
		"code only in the message":       {fakeSearch{codeInMessage: true}, "without error code"},
		"cursor after the last row":      {fakeSearch{extraCursorAtEnd: true}, "next_cursor after all"},
		"negative first total":           {fakeSearch{negativeFirstTotal: true}, "negative total"},
		"empty first page with a cursor": {fakeSearch{emptyFirstPage: true}, "no row can follow"},
		"empty scope ignores offset":     {fakeSearch{emptyIgnoresOffset: true}, "cursor with offset"},
		"forged cursor gives a bad page": {fakeSearch{forgedBadPage: true}, "200 with an invalid page"},
		"forged cursor zeroes the total": {fakeSearch{forgedZeroTotal: true}, "the scope's total is"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkAll(fakeSuite(tc.search))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("checks returned %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

func TestCheckWalkRequiresMoreRowsThanTheLimit(t *testing.T) {
	suite := fakeSuite(fakeSearch{})
	err := CheckWalk(context.Background(), suite, Case{Name: "tiny", Want: []string{"r1", "r2"}})
	if err == nil || !strings.Contains(err.Error(), "seed more than the limit") {
		t.Fatalf("CheckWalk() = %v", err)
	}
}

func TestDecodeItemsRejectsMissingFields(t *testing.T) {
	decode := DecodeItems("items", "id")
	for name, body := range map[string]string{
		"not an object":   `[]`,
		"no items":        `{"total":1,"count":1}`,
		"no total":        `{"items":[],"count":0}`,
		"no count":        `{"items":[],"total":0}`,
		"item without id": `{"items":[{"name":"x"}],"total":1,"count":1}`,
		"cursor not text": `{"items":[],"total":0,"count":0,"next_cursor":3}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(body)); err == nil {
				t.Fatalf("decode(%s) succeeded", body)
			}
		})
	}
	page, err := decode([]byte(`{"items":[{"id":"a"}],"total":2,"count":1,"next_cursor":"n"}`))
	if err != nil || !slices.Equal(page.IDs, []string{"a"}) || page.Total != 2 || page.NextCursor != "n" {
		t.Fatalf("decode() = %+v, %v", page, err)
	}
}

func TestFrameworkCursorMutationsCoverEveryValue(t *testing.T) {
	cursor, err := search.EncodeCursor("t.name:ASC,t.id:ASC", []any{"b", "r1"})
	if err != nil {
		t.Fatal(err)
	}
	forged := FrameworkCursorMutations(cursor)
	if len(forged) < 2*len(adversarialValues) {
		t.Fatalf("%d variants, want at least one per value and adversarial input", len(forged))
	}
	if slices.Contains(forged, cursor) {
		t.Fatal("the original cursor is among the forgeries")
	}
	if FrameworkCursorMutations("not a cursor") != nil {
		t.Fatal("an undecodable cursor produced forgeries")
	}
}

func TestErrorCodesReadsCodeFieldsOnly(t *testing.T) {
	for body, want := range map[string][]string{
		`{"errors":[{"code":"INVALID_CURSOR"}]}`:                   {"INVALID_CURSOR"},
		`{"errors":[{"code":"INVALID\u005fCURSOR"}]}`:              {"INVALID_CURSOR"},
		`{"errors":[{"code":"WRONG","message":"INVALID_CURSOR"}]}`: {"WRONG"},
		`not json`: nil,
	} {
		if got := errorCodes([]byte(body)); !slices.Equal(got, want) {
			t.Errorf("errorCodes(%s) = %v, want %v", body, got, want)
		}
	}
}

func TestFrameworkCursorMutationsKeepUntouchedValuesExact(t *testing.T) {
	big := int64(math.MaxInt64)
	cursor, err := search.EncodeCursor("t.rank:ASC,t.id:ASC", []any{big, "r1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, forged := range FrameworkCursorMutations(cursor) {
		decoded, err := search.DecodeCursor(forged)
		if err != nil || len(decoded.Values) != 2 || decoded.Order != "t.rank:ASC,t.id:ASC" {
			continue
		}
		if string(decoded.Values[1]) != `"r1"` && string(decoded.Values[0]) != "9223372036854775807" {
			t.Fatalf("a forgery of the id changed the untouched rank to %s", decoded.Values[0])
		}
	}
}

func TestValidPage(t *testing.T) {
	for name, tc := range map[string]struct {
		page  Page
		first bool
		ok    bool
	}{
		"a middle page":                  {Page{IDs: []string{"a", "b"}, Total: 5, Count: 2, NextCursor: "n"}, false, true},
		"the last page":                  {Page{IDs: []string{"e"}, Total: 5, Count: 1}, false, true},
		"an empty scope":                 {Page{Total: 0}, true, true},
		"a cursor on an empty page":      {Page{Total: 5, NextCursor: "n"}, false, false},
		"a cursor after every match":     {Page{IDs: []string{"a", "b"}, Total: 2, Count: 2, NextCursor: "n"}, true, false},
		"an empty first page with total": {Page{Total: 5}, true, false},
		"more items than total":          {Page{IDs: []string{"a", "b"}, Total: 1, Count: 2}, false, false},
		"more items than the limit":      {Page{IDs: []string{"a", "b", "c"}, Total: 9, Count: 3}, false, false},
		"count disagrees":                {Page{IDs: []string{"a"}, Total: 9, Count: 42}, false, false},
		"a negative total":               {Page{Total: -1}, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validPage(tc.page, 2, tc.first); (err == nil) != tc.ok {
				t.Fatalf("validPage() = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestRefusalsReportAScopeWithoutAnOffsetCursor(t *testing.T) {
	suite := fakeSuite(fakeSearch{})
	errs := CheckRefusals(context.Background(), suite, Scope{Body: map[string]any{"scope": "empty"}})
	if err := errors.Join(errs...); err == nil || !strings.Contains(err.Error(), "give it SameOrder") {
		t.Fatalf("CheckRefusals() = %v, want a SameOrder configuration error", err)
	}
}

func TestChecksCatchACursorOnASinglePageResult(t *testing.T) {
	f := fakeSearch{cursorOnLastPage: true, rows: fakeRows[:2]}
	errs := CheckRefusals(context.Background(), withDefaults(Suite{Call: f.call}), Scope{Body: map[string]any{}})
	if err := errors.Join(errs...); err == nil || !strings.Contains(err.Error(), "no row can follow") {
		t.Fatalf("CheckRefusals() = %v, want the impossible cursor caught", err)
	}
}
