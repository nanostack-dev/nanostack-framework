package search

import (
	"encoding/json"
	"testing"
)

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

func TestPaginationJSONIsLowercase(t *testing.T) {
	encoded, err := json.Marshal(Pagination{Limit: 5, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"limit":5,"offset":0}` {
		t.Fatalf("json = %s", encoded)
	}
	var decoded Pagination
	if err := json.Unmarshal(
		[]byte(`{"limit":3,"cursor":"abc"}`),
		&decoded,
	); err != nil || decoded.Cursor != "abc" ||
		decoded.Limit != 3 {
		t.Fatalf("decoded = %+v, %v", decoded, err)
	}
}
