package search

import "testing"

type sortField string

func TestNewRequestDefaults(t *testing.T) {
	req := NewRequest[struct{}, sortField]()
	if req.Pagination.Limit != defaultLimit || req.Pagination.Offset != defaultOffset {
		t.Fatalf("unexpected pagination: %+v", req.Pagination)
	}
	if req.Sort == nil {
		t.Fatal("expected sort slice")
	}
}

func TestWithPaginationDefaultsAbsentLimit(t *testing.T) {
	req := NewRequest[struct{}, sortField]().WithPagination(&Pagination{Cursor: "abc"})
	if req.Pagination.Limit != defaultLimit || req.Pagination.Cursor != "abc" {
		t.Fatalf("pagination = %+v", req.Pagination)
	}
}
