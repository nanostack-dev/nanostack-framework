package validate_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

// wideBody is a body with a long member name over a wide array: the shape
// that made a pointer-keyed index cost key length × element count.
func wideBody(keyLen, elems int) []byte {
	var b strings.Builder
	b.WriteString(`{"name":"x","url":"https://a.b","`)
	b.WriteString(strings.Repeat("k", keyLen))
	b.WriteString(`":[`)
	for i := range elems {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('0')
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func indexBytes(t *testing.T, body []byte) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	p := validate.RootOf(body)
	if !p.Field("name").Present(false) {
		t.Fatal("name is present")
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// The index is linear in the body: a long key over a wide array must not
// multiply. The pointer-keyed index allocated 788 MB for this 200 KB body.
func TestDocumentIndexIsLinearInTheBody(t *testing.T) {
	body := wideBody(8000, 100_000)
	got := indexBytes(t, body)
	t.Logf("body %d bytes, index allocated %d bytes", len(body), got)
	if limit := uint64(8 * len(body)); got > limit {
		t.Fatalf("indexing a %d-byte body allocated %d bytes, more than %d", len(body), got, limit)
	}
	// Doubling the key at the same element count must not double the cost.
	longer := indexBytes(t, wideBody(16000, 100_000))
	if longer > got+uint64(64_000) {
		t.Fatalf("a key 8000 bytes longer cost %d more bytes", longer-got)
	}
}

func TestDocumentLookups(t *testing.T) {
	var members strings.Builder
	members.WriteString(`{`)
	for i := range 40 {
		if i > 0 {
			members.WriteByte(',')
		}
		members.WriteString(`"m` + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `":null`)
	}
	members.WriteString(`}`)
	doc := `{"a/b~c":{"x":[0,{"y":null},[1,[null]],"s",{"z":true}]},"big":` + members.String() + `,"s":"v"}`
	p := validate.RootOf([]byte(doc))

	cases := []struct {
		path    validate.Path
		present bool
		null    bool
	}{
		{p.Field("a/b~c").Field("x").Index(1).Field("y"), true, true},
		{p.Field("a/b~c").Field("x").Index(0), true, false},
		{p.Field("a/b~c").Field("x").Index(2).Index(1).Index(0), true, true},
		{p.Field("a/b~c").Field("x").Index(3), true, false},
		{p.Field("a/b~c").Field("x").Index(4).Field("z"), true, false},
		{p.Field("a/b~c").Field("x").Index(5), false, false},
		{p.Field("a/b~c").Field("x").Join("/01"), false, false}, // not a canonical index
		{p.Field("a/b~c").Field("x").Join("/-1"), false, false},
		{p.Field("a").Field("b~c"), false, false},
		{p.Field("s").Field("v"), false, false}, // a scalar has no members
		{p.Field("big").Field("mab"), true, true},
		{p.Field("big").Field("zz"), false, false},
		{p.Join("no-slash"), false, false},
	}
	for _, c := range cases {
		if got := c.path.Present(!c.present); got != c.present {
			t.Errorf("%s: Present = %v, want %v", c.path, got, c.present)
		}
		if got := c.path.IsNull(); got != c.null {
			t.Errorf("%s: IsNull = %v, want %v", c.path, got, c.null)
		}
	}
	if n, ok := p.Field("big").MemberCount(); !ok || n != 40 {
		t.Errorf("big has %d members, %v", n, ok)
	}
	if names, ok := p.Field("s").Members(); !ok || names != nil {
		t.Errorf("a string has no members: %v, %v", names, ok)
	}
}

func TestEmptyContainersAreIndexed(t *testing.T) {
	p := validate.RootOf([]byte(`{"o":{},"a":[],"list":[{},[],{"x":1},null]}`))
	for _, ptr := range []validate.Path{p.Field("o"), p.Field("a"), p.Field("list").Index(0), p.Field("list").Index(1)} {
		if !ptr.Present(false) || ptr.IsNull() {
			t.Errorf("%s: an empty container is present and not null", ptr)
		}
	}
	if names, known := p.Field("list").Index(0).Members(); !known || len(names) != 0 {
		t.Errorf("members of {} = %v, %v", names, known)
	}
	if !p.Field("list").Index(2).Field("x").Present(false) || !p.Field("list").Index(3).IsNull() {
		t.Error("the elements after empty containers keep their place")
	}
	if p.Field("list").Index(1).Field("x").Present(true) {
		t.Error("an empty array has no member")
	}
}
