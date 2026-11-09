// Package apigen holds the request bodies and strict-server request objects
// of openapi.yaml, as nano gen api writes them.
package apigen

import (
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// SortDirection is the shared sort_direction enum.
type SortDirection string

// RouteSearchRequestSortBy is the sort_by enum of RouteSearchRequest.
type RouteSearchRequestSortBy string

// RouteSearchRequest is the body of POST /routes/search.
type RouteSearchRequest struct {
	Filter         *RouteSearchFilter        `json:"filter,omitempty"`
	FullTextSearch *string                   `json:"full_text_search,omitempty"`
	Pagination     *search.Pagination        `json:"pagination,omitempty"`
	SortBy         *RouteSearchRequestSortBy `json:"sort_by,omitempty"`
	SortDirection  *SortDirection            `json:"sort_direction,omitempty"`
}

// RouteSearchFilter is the filter object of RouteSearchRequest.
type RouteSearchFilter struct {
	Names *[]string `json:"names,omitempty"`
	Skus  *[]string `json:"skus,omitempty"`
}

// WindowSearchRequest is the body of POST /windows/search: no sort_by, so
// sort_direction applies to the default sort.
type WindowSearchRequest struct {
	Filter        WindowSearchFilter `json:"filter"`
	SortDirection *SortDirection     `json:"sort_direction,omitempty"`
}

// WindowSearchFilter is the filter object of WindowSearchRequest.
type WindowSearchFilter struct {
	CreatedFrom   *time.Time `json:"created_from,omitempty"`
	CreatedTo     *time.Time `json:"created_to,omitempty"`
	DeletedAfter  *time.Time `json:"deleted_after,omitempty"`
	DeletedBefore *time.Time `json:"deleted_before,omitempty"`
	FolderID      *string    `json:"folder_id,omitempty"`
}

// RecentSearchRequest is the body of POST /recent/search.
type RecentSearchRequest struct {
	Filter         *RecentSearchFilter `json:"filter,omitempty"`
	FullTextSearch *string             `json:"full_text_search,omitempty"`
	Pagination     *search.Pagination  `json:"pagination,omitempty"`
}

// RecentSearchFilter is the filter object of RecentSearchRequest.
type RecentSearchFilter struct {
	Since *time.Time `json:"since,omitempty"`
}

// SearchRoutesRequestObject is the strict-server request of SearchRoutes.
type SearchRoutesRequestObject struct {
	Body *RouteSearchRequest
}

// SearchWindowsRequestObject is the strict-server request of SearchWindows.
type SearchWindowsRequestObject struct {
	Body *WindowSearchRequest
}

// SearchRecentRequestObject is the strict-server request of SearchRecent.
type SearchRecentRequestObject struct {
	Body *RecentSearchRequest
}
