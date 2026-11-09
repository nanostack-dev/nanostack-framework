// Package search is the search contract of Nanostack DB: the page, sort and
// cursor types that every layer shares, the limits of a search, its errors,
// and All, which reads every page of a search.
//
// The package imports the standard library only, so an API, domain or test
// package can import it with no SQL dialect and no database driver. The code
// that runs a search on a database is in other packages of this module.
//
// A search answers a page (Result) of the rows that match its filters. A page
// starts after a cursor (Pagination.Cursor, the NextCursor of the previous
// page) or at an offset, never both. CheckPage is the rule for a page that
// every search applies, for HTTP and Go callers alike.
//
// The errors fall into two groups. A client error (IsClientError) is a request
// the caller can fix: the HTTP edge answers it with a 400, and core/log logs it
// at Debug. ErrUnscoped is a programming error: the HTTP edge answers it with
// a 500, and core/log logs it at Error.
package search
