package validate_test

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

func TestPathEscapesLikeJSONText(t *testing.T) {
	doc := []byte(`{"a/b~c":{"x":[0,{"y":1}]}}`)
	dec := jsontext.NewDecoder(bytes.NewReader(doc))
	var got []string
	for {
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if tok.Kind() == '0' {
			got = append(got, string(dec.StackPointer()))
		}
	}
	want := []string{
		validate.Root.Field("a/b~c").Field("x").Index(0).Pointer(),
		validate.Root.Field("a/b~c").Field("x").Index(1).Field("y").Pointer(),
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("pointers = %q, want %q", got, want)
	}
	if want[0] != "/a~1b~0c/x/0" {
		t.Fatalf("escaped pointer = %q", want[0])
	}
}

func TestPresenceWithDocument(t *testing.T) {
	p := validate.RootOf([]byte(`{"name":"","nick":null,"tags":["a",null],"extra/1":true,"zzz":1}`))
	var errs validate.Errors

	if !errs.Required(p.Field("name"), false) {
		t.Error("name is present")
	}
	if errs.Required(p.Field("count"), true) {
		t.Error("count is absent even though the fallback says present")
	}
	if !errs.Required(p.Field("nick"), false) {
		t.Error("a null member is present")
	}
	if errs.NotNull(p.Field("nick")) {
		t.Error("nick is null")
	}
	if !errs.NotNull(p.Field("tags").Index(0)) || errs.NotNull(p.Field("tags").Index(1)) {
		t.Error("tags/1 is null, tags/0 is not")
	}
	errs.OnlyMembers(p, "name", "nick", "tags")

	want := validate.Errors{
		{Pointer: "/count", Code: "required", Detail: "is required"},
		{Pointer: "/nick", Code: "type", Detail: "must not be null"},
		{Pointer: "/tags/1", Code: "type", Detail: "must not be null"},
		{Pointer: "/extra~11", Code: "additionalProperties", Detail: "is not an allowed property"},
		{Pointer: "/zzz", Code: "additionalProperties", Detail: "is not an allowed property"},
	}
	assertErrors(t, errs, want)
	if n, ok := p.MemberCount(); !ok || n != 5 {
		t.Errorf("MemberCount = %d, %v", n, ok)
	}
}

func TestPresenceWithoutDocument(t *testing.T) {
	p := validate.Root
	var errs validate.Errors
	if !errs.Required(p.Field("a"), true) {
		t.Error("fallback true means present")
	}
	if errs.Required(p.Field("b"), false) {
		t.Error("fallback false means absent")
	}
	if !errs.NotNull(p.Field("c")) {
		t.Error("nothing is null without a document")
	}
	errs.OnlyMembers(p, "a")
	assertErrors(t, errs, validate.Errors{{Pointer: "/b", Code: "required", Detail: "is required"}})
	if _, ok := p.MemberCount(); ok {
		t.Error("member count is unknown without a document")
	}
}

func TestInvalidDocumentFallsBack(t *testing.T) {
	p := validate.RootOf([]byte(`{"a":`))
	if p.HasDocument() {
		t.Fatal("a truncated text must not be used as a document")
	}
	if !p.Field("a").Present(true) {
		t.Error("fallback must answer")
	}
}

func TestFormats(t *testing.T) {
	cases := []struct {
		name  string
		check func(string) bool
		ok    []string
		bad   []string
	}{
		{
			"uuid",
			validate.UUID,
			[]string{"7f9c24e8-3b12-4fef-91e0-3a5b0c6d1e2f", "7F9C24E8-3B12-4FEF-91E0-3A5B0C6D1E2F"},
			[]string{
				"",
				"7f9c24e83b124fef91e03a5b0c6d1e2f",
				"7f9c24e8-3b12-4fef-91e0-3a5b0c6d1e2g",
				"{7f9c24e8-3b12-4fef-91e0-3a5b0c6d1e2f}",
			},
		},
		{
			"email",
			validate.Email,
			[]string{
				"a@b.co",
				"first.last+tag@sub.example.org",
				`"john doe"@example.com`,
				"x@[192.168.0.1]",
				"x@[IPv6:::1]",
			},
			[]string{"", "a", "a@", "@b.co", "a@b", "a..b@c.de", ".a@c.de", "A <a@b.co>", "a@-b.co", "a b@c.de"},
		},
		{"uri", validate.URI,
			[]string{"https://example.com/a?b=c#d", "urn:isbn:0451450523", "mailto:a@b.co", "http://[::1]:80/"},
			[]string{"", "/relative", "example.com", "http://exa mple.com", "http://a/%zz", "1http://a"}},
		{
			"date-time",
			validate.DateTime,
			[]string{"2026-11-07T10:20:30Z", "2026-11-07t10:20:30.123456789+01:00", "2016-12-31T23:59:60Z"},
			[]string{
				"2026-11-07",
				"2026-11-07T10:20:30",
				"2026-11-07 10:20:30Z",
				"2026-02-30T10:20:30Z",
				"2026-11-07T24:00:00Z",
				"2026-11-07T10:20:30.Z",
				"2026-11-07T10:20:30+0100",
			},
		},
		{"date", validate.Date,
			[]string{"2026-11-07", "2024-02-29"},
			[]string{"2026-11-7", "2025-02-29", "2026-13-01", "20261107"}},
		{"ipv4", validate.IPv4,
			[]string{"192.168.0.1", "0.0.0.0"},
			[]string{"192.168.0.01", "256.0.0.1", "::1", "1.2.3"}},
		{"ipv6", validate.IPv6,
			[]string{"::1", "2001:db8::1", "::ffff:1.2.3.4"},
			[]string{"192.168.0.1", "fe80::1%eth0", "2001:db8:::1"}},
		{"hostname", validate.Hostname,
			[]string{"example.com", "a-b.c", "localhost"},
			[]string{"", "-a.com", "a_b.com", "a..b"}},
	}
	for _, c := range cases {
		for _, s := range c.ok {
			if !c.check(s) {
				t.Errorf("%s(%q) = false, want true", c.name, s)
			}
		}
		for _, s := range c.bad {
			if c.check(s) {
				t.Errorf("%s(%q) = true, want false", c.name, s)
			}
		}
	}
}

func TestDecodeErrorMessages(t *testing.T) {
	type body struct {
		Name  string    `json:"name"`
		Count int32     `json:"count"`
		At    time.Time `json:"at"`
		Tags  []string  `json:"tags"`
	}
	cases := []struct {
		in   string
		want validate.Error
	}{
		{`{"name":42}`, validate.Error{Pointer: "/name", Code: "type", Detail: "must be a string, got number"}},
		{`{"count":"x"}`, validate.Error{Pointer: "/count", Code: "type", Detail: "must be an integer, got string"}},
		{`{"count":1.5}`, validate.Error{Pointer: "/count", Code: "type", Detail: "must be an integer"}},
		{
			`{"count":99999999999}`,
			validate.Error{Pointer: "/count", Code: "format", Detail: "is out of range for int32"},
		},
		{
			`{"at":"yesterday"}`,
			validate.Error{Pointer: "/at", Code: "format", Detail: "must be an RFC 3339 date-time"},
		},
		{
			`{"tags":["a",{}]}`,
			validate.Error{Pointer: "/tags/1", Code: "type", Detail: "must be a string, got object"},
		},
	}
	for _, c := range cases {
		var b body
		err := jsonv2.Unmarshal([]byte(c.in), &b, validate.DecodeOptions())
		got, ok := validate.DecodeError(validate.Root.Field("body"), err)
		c.want.Pointer = "/body" + c.want.Pointer
		if !ok || got != c.want {
			t.Errorf("%s: got %+v (%v), want %+v; err=%v", c.in, got, ok, c.want, err)
		}
	}
	if _, ok := validate.DecodeError(validate.Root, io.EOF); ok {
		t.Error("only semantic errors convert")
	}
}

func TestDecodeOptionsAreStrict(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	for _, in := range []string{`{"name":"a","name":"b"}`, "{\"name\":\"\xff\"}"} {
		if err := jsonv2.Unmarshal([]byte(in), &v, validate.DecodeOptions()); err == nil {
			t.Errorf("%q decoded", in)
		}
	}
	v.Name = ""
	if err := jsonv2.Unmarshal([]byte(`{"NAME":"a"}`), &v, validate.DecodeOptions()); err != nil || v.Name != "" {
		t.Errorf("names must match case-sensitively: %v %q", err, v.Name)
	}
}

func TestVariantAndDiscriminator(t *testing.T) {
	type basic struct {
		Type string `json:"type"`
		User string `json:"user"`
	}
	p := validate.Root.Field("auth")
	if _, errs := validate.Variant[basic](
		[]byte(`{"type":"basic","user":1}`),
		p,
	); len(errs) != 1 ||
		errs[0].Pointer != "/auth/user" {
		t.Errorf("variant errors = %v", errs)
	}
	var errs validate.Errors
	if v, ok := errs.Discriminator([]byte(`{"type":"basic"}`), p, "type"); !ok || v != "basic" {
		t.Errorf("discriminator = %q %v", v, ok)
	}
	errs.Discriminator([]byte(`{}`), p, "type")
	errs.Discriminator([]byte(`{"type":3}`), p, "type")
	assertErrors(t, errs, validate.Errors{
		{Pointer: "/auth/type", Code: "required", Detail: "is required"},
		{Pointer: "/auth/type", Code: "type", Detail: "must be a string"},
	})
}

func TestCollections(t *testing.T) {
	if i, j, ok := validate.Unique([]string{"a", "b", "a"}); ok || i != 0 || j != 2 {
		t.Errorf("Unique = %d %d %v", i, j, ok)
	}
	type item struct{ Tags []string }
	if _, _, ok := validate.UniqueJSON([]item{{[]string{"a"}}, {[]string{"a"}}}); ok {
		t.Error("equal structs are duplicates")
	}
	if _, _, ok := validate.UniqueJSON([]map[string]int{{"a": 1, "b": 2}, {"b": 2, "a": 1}}); ok {
		t.Error("key order does not matter")
	}
	for _, c := range []struct {
		v    float64
		m    string
		want bool
	}{{0.3, "0.1", true}, {10, "5", true}, {7, "2", false}, {0.35, "0.1", false}, {math.Inf(1), "1", false}} {
		if got := validate.MultipleOf(c.v, c.m); got != c.want {
			t.Errorf("MultipleOf(%v, %s) = %v", c.v, c.m, got)
		}
	}
	if !validate.MultipleOf(float32(0.3), "0.1") || !validate.MultipleOf(int32(-10), "5") ||
		validate.MultipleOf(uint8(7), "2") {
		t.Error("MultipleOf must honour the value's own precision")
	}
	if !validate.JSONType(float64(2), "integer") || validate.JSONType(2.5, "integer") ||
		!validate.JSONType(nil, "string", "null") {
		t.Error("JSONType")
	}
}

func assertErrors(t *testing.T, got, want validate.Errors) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("errors = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("errors[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

type priority int

// UnmarshalJSONFrom is the method nano gen validate writes for a named
// integer type.
func (v *priority) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return validate.DecodeInteger(dec, v)
}

type integers struct {
	A int      `json:"a"`
	B int32    `json:"b"`
	C *int64   `json:"c"`
	D uint8    `json:"d"`
	E priority `json:"e"`
	F float64  `json:"f"`
	G []int    `json:"g"`
	H int64    `json:"h"`
}

// JSON Schema defines integer by value: 3.0 and 1e2 are integers.
func TestIntegralNumbersDecodeIntoIntegers(t *testing.T) {
	var v integers
	in := `{"a":3.0,"b":1e2,"c":-1.50e1,"d":2.000,"e":5E0,"f":3.0,"g":[0.0,1,10e-1,-0.0],"h":9223372036854775807}`
	if err := jsonv2.Unmarshal([]byte(in), &v, validate.DecodeOptions()); err != nil {
		t.Fatal(err)
	}
	if v.A != 3 || v.B != 100 || v.C == nil || *v.C != -15 || v.D != 2 || v.E != 5 || v.F != 3 ||
		len(v.G) != 4 || v.G[0] != 0 || v.G[1] != 1 || v.G[2] != 1 || v.G[3] != 0 || v.H != math.MaxInt64 {
		t.Fatalf("decoded %+v", v)
	}
}

func TestNonIntegralNumbersStillFail(t *testing.T) {
	cases := map[string]validate.Error{
		`{"a":3.5}`:                 {Pointer: "/a", Code: "type", Detail: "must be an integer"},
		`{"a":1e-1}`:                {Pointer: "/a", Code: "type", Detail: "must be an integer"},
		`{"b":3e9}`:                 {Pointer: "/b", Code: "format", Detail: "is out of range for int32"},
		`{"c":1e30}`:                {Pointer: "/c", Code: "format", Detail: "is out of range for int64"},
		`{"a":1e999999999}`:         {Pointer: "/a", Code: "format", Detail: "is out of range for int"},
		`{"d":-1.0}`:                {Pointer: "/d", Code: "format", Detail: "is out of range for uint8"},
		`{"e":2.5}`:                 {Pointer: "/e", Code: "type", Detail: "must be an integer"},
		`{"g":[1,2.5]}`:             {Pointer: "/g/1", Code: "type", Detail: "must be an integer"},
		`{"a":"3"}`:                 {Pointer: "/a", Code: "type", Detail: "must be an integer, got string"},
		`{"e":"3"}`:                 {Pointer: "/e", Code: "type", Detail: "must be an integer, got string"},
		`{"e":1e19}`:                {Pointer: "/e", Code: "format", Detail: "is out of range for int"},
		`{"h":9223372036854775808}`: {Pointer: "/h", Code: "format", Detail: "is out of range for int64"},
		`{"d":256}`:                 {Pointer: "/d", Code: "format", Detail: "is out of range for uint8"},
	}
	for in, want := range cases {
		var v integers
		err := jsonv2.Unmarshal([]byte(in), &v, validate.DecodeOptions())
		got, ok := validate.DecodeError(validate.Root, err)
		if !ok || got != want {
			t.Errorf("%s: %+v (%v), want %+v", in, got, err, want)
		}
	}
}
