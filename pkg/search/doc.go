// Package search holds the request, page and sort types of hand-written
// searches. The search contract is db/search: the errors, IsClientError, the
// cursor functions and Pagination.Validate of this package are those of
// db/search, so both packages give one answer for a search error and issue one
// cursor format.
package search
