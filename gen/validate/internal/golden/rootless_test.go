package golden_test

import (
	"testing"

	"github.com/oapi-codegen/nullable"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/additional"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/arrays"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nested"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nullable31"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nullabletype"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/required"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/unions"
)

// Without a document (validate.Root), validators judge presence from the Go
// value: a nil pointer, slice or map is absent, a value field is present,
// and nothing is null or unknown.
func TestValidateWithoutDocument(t *testing.T) {
	ptr := func(s string) *string { return &s }
	auth := unions.Auth{}
	if err := auth.FromBasic(unions.Basic{Type: "basic", User: ""}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		v    interface {
			Validate(validate.Path) validate.Errors
		}
		want []string
	}{
		{"nil slice is absent; value fields are present", required.Body{}, []string{"/tags required"}},
		{
			"constraints on set values",
			required.Body{Tags: []string{}, Count: -1, Note: ptr("abcd")},
			[]string{"/count minimum", "/note maxLength"},
		},
		{"a nil required nullable pointer may be null", nullable31.Body{Must: "ab"}, nil},
		{"Nullable knows absence without a document", nullabletype.Body{}, []string{"/maybe required"}},
		{"Nullable null is present", nullabletype.Body{Maybe: nullable.NewNullNullable[string]()}, nil},
		{"unknown members cannot exist in a struct", additional.Body{}, nil},
		{"uniqueItems", arrays.Body{Tags: &[]string{"a", "a"}}, []string{"/tags uniqueItems"}},
		{"zero nested struct is present", nested.Body{}, []string{"/home/city minLength"}},
		{"union built in Go", unions.Body{Auth: auth}, []string{"/auth/user minLength"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, e := range c.v.Validate(validate.Root) {
				got = append(got, e.Pointer+" "+e.Code)
			}
			if !equal(got, c.want) {
				t.Fatalf("errors = %q, want %q", got, c.want)
			}
		})
	}
}
