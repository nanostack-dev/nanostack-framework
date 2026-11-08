package nanocache_test

import (
	"context"
	"testing"
)

// Free-text parts must address exactly one key and one scope: ':' and '\'
// are escaped in the key, and the glob characters are escaped in the
// EvictPrefix pattern, so a part never acts as a separator or a wildcard.
func TestKeyPartsHaveOneEncoding(t *testing.T) {
	products := newProductCache(newFakeStore())
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"a:b"}, `product:a\:b`},
		{[]string{"a", "b"}, `product:a:b`},
		{[]string{`a\`, "b"}, `product:a\\:b`},
		{[]string{`a\:b`}, `product:a\\\:b`},
		{[]string{"team[ops]", "*", "?"}, `product:team[ops]:*:?`},
		{[]string{"5b0e7c1e-1f0a-4c55-9d57-8a1f5a4cbe11", "3f9a"}, "product:5b0e7c1e-1f0a-4c55-9d57-8a1f5a4cbe11:3f9a"},
	} {
		if got := products.Key(tc.parts...).String(); got != tc.want {
			t.Errorf("Key(%q) = %q, want %q", tc.parts, got, tc.want)
		}
	}
	if products.Key("a:b").String() == products.Key("a", "b").String() {
		t.Fatal(`Key("a:b") and Key("a", "b") address the same Redis key`)
	}
}

func TestEvictPrefixPatternMatchesOnlyItsScope(t *testing.T) {
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"team[ops]"}, `product:team\[ops\]:*`},
		{[]string{"*"}, `product:\*:*`},
		{[]string{"?"}, `product:\?:*`},
		{[]string{`a\b`}, `product:a\\\\b:*`},
		{[]string{"a:b"}, `product:a\\:b:*`},
		{nil, `product:*`},
	} {
		store := newFakeStore()
		if err := newProductCache(store).EvictPrefix(context.Background(), tc.parts...); err != nil {
			t.Fatal(err)
		}
		if len(store.patterns) != 1 || store.patterns[0] != tc.want {
			t.Errorf("EvictPrefix(%q) pattern = %q, want %q", tc.parts, store.patterns, tc.want)
		}
	}
}
