package golden_test

import (
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/arrays"
)

// An array past maxItems fails on maxItems alone: its items are not walked.
func TestItemsPastMaxItemsAreNotChecked(t *testing.T) {
	tags := []string{"abcde", "a", "a", "b"}
	errs := arrays.Body{Tags: &tags}.Validate(validate.Root)
	if len(errs) != 1 || errs[0].Code != validate.CodeMaxItems {
		t.Fatalf("errors = %v, want only maxItems", errs)
	}
}

// The walk over a long array stops once the accumulator is full, nested
// validators included: they write into the caller's accumulator.
func TestItemsStopWhenTheAccumulatorIsFull(t *testing.T) {
	n := 50 * validate.MaxErrors
	body := `{"items":[{}` + strings.Repeat(",{}", n-1) + `]}`
	items := make([]arrays.Item, n)
	for i := range items {
		items[i].Sku = string(rune('a' + i%26)) // distinct enough: uniqueItems is not the point
	}
	errs := arrays.Body{Items: &items}.Validate(validate.RootOf([]byte(body)))
	if len(errs) != validate.MaxErrors+1 || !errs.Truncated() {
		t.Fatalf("len = %d, want %d", len(errs), validate.MaxErrors+1)
	}
}
