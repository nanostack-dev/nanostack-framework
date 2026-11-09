package search_test

import (
	"errors"
	"fmt"
	"testing"

	dbsearch "github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// TestErrorsAreTheContractErrors pins that pkg/search shares the error values
// of db/search, so errors.Is, the error envelope and the log level agree
// whichever package a search returns from.
func TestErrorsAreTheContractErrors(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name  string
		local error
		db    error
	}{
		{"invalid cursor", search.ErrInvalidCursor, dbsearch.ErrInvalidCursor},
		{"cursor with offset", search.ErrCursorWithOffset, dbsearch.ErrCursorWithOffset},
		{"unknown sort", search.ErrUnknownSort, dbsearch.ErrUnknownSort},
	}
	for _, p := range pairs {
		if p.local != p.db { //nolint:errorlint // the values themselves must be equal, not only match
			t.Errorf("%s: pkg/search and db/search hold different values", p.name)
		}
		wrapped := fmt.Errorf("search flows: %w", p.db)
		if !errors.Is(wrapped, p.local) || !errors.Is(fmt.Errorf("x: %w", p.local), p.db) {
			t.Errorf("%s: errors.Is does not match across the packages", p.name)
		}
	}
}

func TestIsClientErrorAgreesWithTheContract(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		dbsearch.ErrInvalidCursor,
		dbsearch.ErrCursorWithOffset,
		dbsearch.ErrUnknownSort,
		dbsearch.ErrInvalidLimit,
		dbsearch.ErrInvalidOffset,
		dbsearch.ErrTooManyValues,
		dbsearch.ErrTermTooLong,
		dbsearch.ErrInvalidSortDirection,
		dbsearch.ErrUnscoped,
		errors.Join(dbsearch.ErrTooManyValues, dbsearch.ErrUnscoped),
		errors.New("connection refused"),
		nil,
	} {
		wrapped := fmt.Errorf("search flows: %w", err)
		if search.IsClientError(wrapped) != dbsearch.IsClientError(wrapped) {
			t.Errorf("IsClientError(%v) differs between pkg/search and db/search", wrapped)
		}
	}
	if search.IsClientError(dbsearch.ErrUnscoped) {
		t.Error("pkg/search.IsClientError(ErrUnscoped) = true, want false")
	}
	if search.IsClientError(errors.Join(dbsearch.ErrInvalidCursor, dbsearch.ErrUnscoped)) {
		t.Error("pkg/search.IsClientError(join with ErrUnscoped) = true, want false")
	}
}

// goldenCursor was issued by pkg/search before the error values and the
// cursor functions moved to db/search (nanostack-framework 64955dc).
const goldenCursor = "eyJ2IjoxLCJvIjoidC5jcmVhdGVkX2F0OkRFU0MsdC5pZDpERVNDIiwiayI6WyIyMDI2LTEwLTE1VDEwOjAwOjAwLjEy" +
	"MzQ1NloiLG51bGwsN119"

// TestCursorIsByteIdenticalAcrossThePackages pins that a cursor that one
// package issues, the other continues, and that neither changed the wire.
func TestCursorIsByteIdenticalAcrossThePackages(t *testing.T) {
	t.Parallel()
	order := "t.created_at:DESC,t.id:DESC"
	values := []any{"2026-10-15T10:00:00.123456Z", nil, int64(7)}
	local, err := search.EncodeCursor(order, values)
	if err != nil {
		t.Fatalf("pkg/search.EncodeCursor() error = %v", err)
	}
	db, err := dbsearch.EncodeCursor(order, values)
	if err != nil {
		t.Fatalf("db/search.EncodeCursor() error = %v", err)
	}
	if local != goldenCursor || db != goldenCursor {
		t.Fatalf(
			"cursors differ from the golden:\npkg/search %s\ndb/search  %s\ngolden     %s",
			local,
			db,
			goldenCursor,
		)
	}
	fromDB, err := search.DecodeCursor(db)
	if err != nil {
		t.Fatalf("pkg/search.DecodeCursor(db/search cursor) error = %v", err)
	}
	fromLocal, err := dbsearch.DecodeCursor(local)
	if err != nil {
		t.Fatalf("db/search.DecodeCursor(pkg/search cursor) error = %v", err)
	}
	if fromDB.Order != order || fromLocal.Order != order || len(fromDB.Values) != len(fromLocal.Values) {
		t.Fatalf("decoded cursors differ: %+v, %+v", fromDB, fromLocal)
	}
	for i := range fromDB.Values {
		if string(fromDB.Values[i]) != string(fromLocal.Values[i]) {
			t.Fatalf("Values[%d] = %s and %s", i, fromDB.Values[i], fromLocal.Values[i])
		}
	}
	if _, decodeErr := search.DecodeCursor("!"); !errors.Is(decodeErr, dbsearch.ErrInvalidCursor) {
		t.Fatalf("pkg/search.DecodeCursor(\"!\") error = %v, want db/search.ErrInvalidCursor", decodeErr)
	}
}

func TestValidateAgreesWithTheContract(t *testing.T) {
	t.Parallel()
	for _, p := range []search.Pagination{
		{Limit: 10},
		{Limit: 10, Offset: 3},
		{Limit: 10, Cursor: "abc"},
		{Limit: 10, Offset: 1, Cursor: "abc"},
		{Limit: 10, Cursor: string(make([]byte, search.MaxCursorLength+1))},
	} {
		got := p.Validate()
		want := dbsearch.Pagination{Limit: p.Limit, Offset: p.Offset, Cursor: p.Cursor}.Validate()
		if (got == nil) != (want == nil) || (got != nil && got.Error() != want.Error()) {
			t.Errorf("Validate(%+v) = %v, db/search gives %v", p, got, want)
		}
	}
	if search.MaxCursorLength != dbsearch.MaxCursorLength {
		t.Errorf("MaxCursorLength = %d, db/search has %d", search.MaxCursorLength, dbsearch.MaxCursorLength)
	}
}
