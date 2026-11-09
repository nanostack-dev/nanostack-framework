package search_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

type sortField string

func TestNewRequestDefaults(t *testing.T) {
	t.Parallel()
	req := search.NewRequest[struct{}, sortField]()
	if req.Pagination.Limit != 10 || req.Pagination.Offset != 0 {
		t.Fatalf("unexpected pagination: %+v", req.Pagination)
	}
	if req.Sort == nil {
		t.Fatal("expected sort slice")
	}
}

func TestWithPaginationDefaultsAbsentLimit(t *testing.T) {
	t.Parallel()
	req := search.NewRequest[struct{}, sortField]().WithPagination(&search.Pagination{Cursor: "abc"})
	if req.Pagination.Limit != 10 || req.Pagination.Cursor != "abc" {
		t.Fatalf("pagination = %+v", req.Pagination)
	}
}

func TestWithCursorClearsOffset(t *testing.T) {
	t.Parallel()
	req := search.NewRequest[struct{}, sortField]().
		WithPagination(&search.Pagination{Limit: 5, Offset: 20}).
		WithCursor("abc")
	if req.Pagination.Offset != 0 || req.Pagination.Cursor != "abc" || req.Pagination.Limit != 5 {
		t.Fatalf("pagination = %+v", req.Pagination)
	}
	one := req.One()
	if one.Pagination != (search.Pagination{Limit: 1}) {
		t.Fatalf("One() pagination = %+v", one.Pagination)
	}
}

func TestWithSortDefaults(t *testing.T) {
	t.Parallel()
	req := search.NewRequest[struct{}, sortField]().WithSort(nil, nil)
	want := search.Sort[sortField]{Field: "created_at", Direction: search.SortAscending}
	if len(req.Sort) != 1 || req.Sort[0] != want {
		t.Fatalf("sort = %+v", req.Sort)
	}
}
