package search_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

func TestCheckPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		page search.Pagination
		want error
	}{
		{"smallest page", search.Pagination{Limit: 1}, nil},
		{"largest page", search.Pagination{Limit: search.MaxLimit}, nil},
		{"offset", search.Pagination{Limit: 10, Offset: 40}, nil},
		{"cursor", search.Pagination{Limit: 10, Cursor: "abc"}, nil},
		{"zero limit", search.Pagination{}, search.ErrInvalidLimit},
		{"negative limit", search.Pagination{Limit: -1}, search.ErrInvalidLimit},
		{"limit over the maximum", search.Pagination{Limit: search.MaxLimit + 1}, search.ErrInvalidLimit},
		{"negative offset", search.Pagination{Limit: 10, Offset: -1}, search.ErrInvalidOffset},
		{"cursor with offset", search.Pagination{Limit: 10, Offset: 1, Cursor: "abc"}, search.ErrCursorWithOffset},
		{"long cursor", search.Pagination{Limit: 10, Cursor: strings.Repeat("a", search.MaxCursorLength+1)},
			search.ErrInvalidCursor},
		// The limit is checked first, so a request with two faults names the page size.
		{"limit before cursor", search.Pagination{Offset: 1, Cursor: "abc"}, search.ErrInvalidLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := search.CheckPage(tt.page)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("CheckPage(%+v) = %v, want nil", tt.page, err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("CheckPage(%+v) = %v, want %v", tt.page, err, tt.want)
			}
			if !search.IsClientError(err) {
				t.Fatalf("CheckPage(%+v) = %v, which is not a client error", tt.page, err)
			}
		})
	}
}

func TestCheckPageNamesTheValue(t *testing.T) {
	t.Parallel()
	if err := search.CheckPage(search.Pagination{Limit: 5000}); !strings.Contains(err.Error(), "5000") {
		t.Fatalf("CheckPage error %q does not name the limit", err)
	}
	if err := search.CheckPage(search.Pagination{Limit: 1, Offset: -3}); !strings.Contains(err.Error(), "-3") {
		t.Fatalf("CheckPage error %q does not name the offset", err)
	}
}

func TestPaginationValidate(t *testing.T) {
	t.Parallel()
	// Validate keeps the rules of pkg/search: it checks only the cursor. The
	// limit and the offset bounds are CheckPage's and the struct tags'.
	if err := (search.Pagination{}).Validate(); err != nil {
		t.Fatalf("zero pagination: %v", err)
	}
	if err := (search.Pagination{Limit: 10, Offset: 20}).Validate(); err != nil {
		t.Fatalf("offset only: %v", err)
	}
	if err := (search.Pagination{Limit: 10, Cursor: "abc"}).Validate(); err != nil {
		t.Fatalf("cursor only: %v", err)
	}
	err := (search.Pagination{Limit: 10, Offset: 1, Cursor: "abc"}).Validate()
	if !errors.Is(err, search.ErrCursorWithOffset) {
		t.Fatalf("cursor with offset: %v, want ErrCursorWithOffset", err)
	}
	long := search.Pagination{Limit: 10, Cursor: strings.Repeat("a", search.MaxCursorLength+1)}
	if longErr := long.Validate(); !errors.Is(longErr, search.ErrInvalidCursor) {
		t.Fatalf("long cursor: %v, want ErrInvalidCursor", longErr)
	}
}

func TestPaginationJSONIsLowercase(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(search.Pagination{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"limit":5,"offset":0}` {
		t.Fatalf("json = %s", encoded)
	}
	var decoded search.Pagination
	if decodeErr := json.Unmarshal([]byte(`{"limit":3,"cursor":"abc"}`), &decoded); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if decoded != (search.Pagination{Limit: 3, Cursor: "abc"}) {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestResultHasMore(t *testing.T) {
	t.Parallel()
	if (search.Result[int]{}).HasMore() {
		t.Fatal("empty NextCursor reported more")
	}
	if !(search.Result[int]{NextCursor: "x"}).HasMore() {
		t.Fatal("NextCursor did not report more")
	}
}

func TestLimits(t *testing.T) {
	t.Parallel()
	// The values are part of the API contract: a product's OpenAPI maximum
	// may be lower, never higher (gen error A9). A change here is a change of
	// what every search accepts.
	for name, got := range map[string]int{
		"MaxLimit":        search.MaxLimit,
		"MaxCursorLength": search.MaxCursorLength,
		"MaxInValues":     search.MaxInValues,
		"MaxTermLength":   search.MaxTermLength,
	} {
		want := map[string]int{
			"MaxLimit": 1000, "MaxCursorLength": 8192, "MaxInValues": 1000, "MaxTermLength": 1000,
		}[name]
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

func TestSortDirectionValues(t *testing.T) {
	t.Parallel()
	// The values are the wire values of the API and of the validate tag.
	if search.SortAscending != "ASC" || search.SortDescending != "DESC" {
		t.Fatalf("SortDirection values = %q, %q", search.SortAscending, search.SortDescending)
	}
}
