package search_test

import (
	"testing"

	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/validate"
)

// searchWebhooksBody has the shape that oapi-codegen writes for a product
// body whose schema has x-go-type: search.Pagination and search.SortDirection
// with x-go-type-import on pkg/search.
type searchWebhooksBody struct {
	Pagination     search.Pagination     `json:"pagination"                 validate:"required"`
	SortDirection  *search.SortDirection `json:"sort_direction,omitempty"`
	FullTextSearch *string               `json:"full_text_search,omitempty"`
}

type webhookSort string

// TestProductBodyIsTheContract pins that a product type written against
// pkg/search passes to code written against db/search with no conversion.
func TestProductBodyIsTheContract(t *testing.T) {
	t.Parallel()
	body := searchWebhooksBody{Pagination: search.Pagination{Limit: 5, Cursor: "abc"}}
	page := func(p dbsearch.Pagination) dbsearch.Pagination { return p }(body.Pagination)
	if page.Limit != 5 || page.Cursor != "abc" {
		t.Fatalf("page = %+v", page)
	}
	req := func(r dbsearch.Request[struct{}, webhookSort]) dbsearch.Request[struct{}, webhookSort] { return r }(
		search.NewRequest[struct{}, webhookSort]().WithPagination(&body.Pagination).WithSort(nil, body.SortDirection),
	)
	res := func(r search.Result[int]) search.Result[int] { return r }(dbsearch.Result[int]{Items: []int{1}, Count: 1})
	if req.Pagination.Limit != 5 || req.Sort[0].Direction != dbsearch.SortAscending || res.HasMore() {
		t.Fatalf("req = %+v, res = %+v", req, res)
	}
}

// TestValidateStructRefusesZeroLimit pins that the validate tags come with
// the aliases: a page of limit 0 is refused through pkg/search.
func TestValidateStructRefusesZeroLimit(t *testing.T) {
	t.Parallel()
	if err := validate.ValidateStruct(searchWebhooksBody{Pagination: search.Pagination{Limit: 0}}); err == nil {
		t.Fatal("ValidateStruct(Limit: 0) = nil, want an error")
	}
	if err := validate.ValidateStruct(searchWebhooksBody{Pagination: search.Pagination{Limit: 1}}); err != nil {
		t.Fatalf("ValidateStruct(Limit: 1) = %v, want nil", err)
	}
	req := search.NewRequest[struct{}, webhookSort]()
	req.Pagination.Limit = 0
	if err := validate.ValidateStruct(req); err == nil {
		t.Fatal("ValidateStruct(Request with Limit: 0) = nil, want an error")
	}
}
