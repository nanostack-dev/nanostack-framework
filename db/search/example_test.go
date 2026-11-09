package search_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

func ExampleCheckPage() {
	fmt.Println(search.CheckPage(search.Pagination{Limit: 20}))
	err := search.CheckPage(search.Pagination{Limit: 5000})
	fmt.Println(errors.Is(err, search.ErrInvalidLimit), search.IsClientError(err))
	// Output:
	// <nil>
	// true true
}

func ExampleAll() {
	items, err := search.All(context.Background(), numbers{n: 3}, pageOfNumbers)
	fmt.Println(items, err)
	// Output: [1 2 3] <nil>
}
