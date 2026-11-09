package search_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// numbers is an immutable search over 1..n, two items per page.
type numbers struct {
	n      int
	cursor string
}

func (q numbers) After(cursor string) numbers {
	q.cursor = cursor
	return q
}

func pageOfNumbers(_ context.Context, q numbers) (search.Result[int], error) {
	start := 1
	if q.cursor != "" {
		last, err := strconv.Atoi(q.cursor)
		if err != nil {
			return search.Result[int]{}, search.ErrInvalidCursor
		}
		start = last + 1
	}
	items := []int{}
	for i := start; i <= q.n && len(items) < 2; i++ {
		items = append(items, i)
	}
	result := search.Result[int]{Items: items, Total: int64(q.n), Count: len(items)}
	if len(items) > 0 && items[len(items)-1] < q.n {
		result.NextCursor = strconv.Itoa(items[len(items)-1])
	}
	return result, nil
}

func TestAllReadsEveryPage(t *testing.T) {
	t.Parallel()
	q := numbers{n: 5}
	got, err := search.All(t.Context(), q, pageOfNumbers)
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
	if q.cursor != "" {
		t.Fatalf("All() changed the caller's search: %+v", q)
	}
}

func TestAllOfNoMatchIsEmptyNotNil(t *testing.T) {
	t.Parallel()
	got, err := search.All(t.Context(), numbers{}, pageOfNumbers)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("All() = %#v, %v; want an empty slice", got, err)
	}
}

func TestAllStopsAtTheFirstError(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection refused")
	calls := 0
	got, err := search.All(
		t.Context(),
		numbers{n: 9},
		func(ctx context.Context, q numbers) (search.Result[int], error) {
			calls++
			if calls == 2 {
				return search.Result[int]{}, boom
			}
			return pageOfNumbers(ctx, q)
		},
	)
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("All() = %v, %v; want nil, %v", got, err, boom)
	}
	if calls != 2 {
		t.Fatalf("page ran %d times, want 2", calls)
	}
}

func TestAllStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	got, err := search.All(ctx, numbers{n: 9}, func(ctx context.Context, q numbers) (search.Result[int], error) {
		calls++
		cancel() // the caller goes away after the first page
		return pageOfNumbers(ctx, q)
	})
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("All() = %v, %v; want nil, context.Canceled", got, err)
	}
	if calls != 1 {
		t.Fatalf("page ran %d times after the cancel, want 1", calls)
	}
}

// TestAllStopsWhenAPageDoesNotAdvance pins the guard against a hand-written
// Pager that gives back the cursor it started after: without it, All reads the
// same page until ctx ends.
func TestAllStopsWhenAPageDoesNotAdvance(t *testing.T) {
	t.Parallel()
	calls := 0
	got, err := search.All(t.Context(), numbers{n: 9}, func(_ context.Context, _ numbers) (search.Result[int], error) {
		calls++
		return search.Result[int]{Items: []int{1}, Count: 1, NextCursor: "1"}, nil
	})
	if err == nil || got != nil {
		t.Fatalf("All() = %v, %v; want nil and an error", got, err)
	}
	if search.IsClientError(err) {
		t.Fatalf("All() error %v is a client error; a stuck pager is a programming error", err)
	}
	if calls != 2 {
		t.Fatalf("page ran %d times, want 2", calls)
	}
}
