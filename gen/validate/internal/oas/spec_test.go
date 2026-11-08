package oas

import (
	"strings"
	"testing"
)

func TestParseSpecNormalisesVersions(t *testing.T) {
	v30 := `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: number
      minimum: 1
      exclusiveMinimum: true
      maximum: 5
      nullable: true
    R:
      $ref: '#/components/schemas/A'
      maxLength: 3
`
	s, err := ParseSpec("v30", []byte(v30))
	if err != nil {
		t.Fatal(err)
	}
	a := s.lookupSchema(t, "A")
	if a.Minimum != nil || a.ExclusiveMinimum == nil || a.ExclusiveMinimum.Lit != "1" || a.Maximum.Lit != "5" ||
		!a.Nullable {
		t.Fatalf("3.0 schema = %+v", a)
	}
	v31 := strings.Replace(strings.Replace(v30, "3.0.3", "3.1.0", 1),
		"      minimum: 1\n      exclusiveMinimum: true\n", "      exclusiveMinimum: 1\n", 1)
	v31 = strings.Replace(v31, "type: number", "type: [number, \"null\"]", 1)
	v31 = strings.Replace(v31, "      nullable: true\n", "", 1)
	s, err = ParseSpec("v31", []byte(v31))
	if err != nil {
		t.Fatal(err)
	}
	a = s.lookupSchema(t, "A")
	if a.ExclusiveMinimum == nil || a.ExclusiveMinimum.Lit != "1" || !a.Nullable || len(a.Types) != 1 ||
		a.Types[0] != "number" {
		t.Fatalf("3.1 schema = %+v", a)
	}
}

func (s *Spec) lookupSchema(t *testing.T, name string) *Schema {
	t.Helper()
	ref := "#/components/schemas/" + name
	s.parseRef(ref, "test", 0)
	sc := s.schemas[ref]
	if sc == nil {
		t.Fatalf("schema %s not parsed", name)
	}
	return sc
}

// Waivers that accept no gap come back in a stable order, even when a flow
// mapping puts two of them on one line.
func TestUnusedWaiversAreSorted(t *testing.T) {
	spec := `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Z: {type: object, x-nano-unenforced: z, properties: {b: {type: string, x-nano-unenforced: b}}}
    A:
      type: string
      x-nano-unenforced: a
`
	for range 20 {
		s, err := ParseSpec("spec", []byte(spec))
		if err != nil {
			t.Fatal(err)
		}
		s.lookupSchema(t, "Z")
		s.lookupSchema(t, "A")
		var got []string
		for _, w := range s.Unused() {
			got = append(got, w.Loc)
		}
		want := "#/components/schemas/Z #/components/schemas/Z/properties/b #/components/schemas/A"
		if strings.Join(got, " ") != want {
			t.Fatalf("Unused = %q, want %q", got, want)
		}
	}
}
