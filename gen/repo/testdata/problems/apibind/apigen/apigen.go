// Package apigen holds the request bodies of openapi.yaml, as nanostack gen api
// writes them.
package apigen

import "github.com/nanostack-dev/nanostack-framework/db/search"

// SortDirection is the shared sort_direction enum.
type SortDirection string

// BadSortRequest has a sort_by value with no sort, misses a sort, accepts
// a property that nothing reads, and has a page that disagrees.
type BadSortRequest struct {
	Extra         *string            `json:"extra,omitempty"`
	Pagination    *search.Pagination `json:"pagination,omitempty"`
	SortBy        *string            `json:"sort_by,omitempty"`
	SortDirection *SortDirection     `json:"sort_direction,omitempty"`
}

// BadFilterRequest has a filter property with no filter, a list above the
// runtime's limit, and an inherited full_text_search that nothing reads.
type BadFilterRequest struct {
	Filter         *BadFilter     `json:"filter,omitempty"`
	FullTextSearch *string        `json:"full_text_search,omitempty"`
	SortDirection  *SortDirection `json:"sort_direction,omitempty"`
}

// BadFilter is the filter object of BadFilterRequest.
type BadFilter struct {
	IPAddresses *[]string `json:"ip_addresses,omitempty"`
	IDs         *[]string `json:"ids,omitempty"`
}

// OwnTextRequest declares full_text_search in its own schema, and no
// operation takes it.
type OwnTextRequest struct {
	FullTextSearch *string `json:"full_text_search,omitempty"`
}

// NoTextRequest has no full_text_search.
type NoTextRequest struct {
	Pagination *search.Pagination `json:"pagination,omitempty"`
}
