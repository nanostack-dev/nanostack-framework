package genapi

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// plainTypes removes the hints from every place of the fixture and keeps
// x-go-type: string and a skip-optional-pointer without a nullable type.
func TestPlainTypesRemovesTheHints(t *testing.T) {
	doc, err := loadSpec(filepath.Join("testdata", "plaintypes", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plainTypes(doc)
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for hint, want := range map[string]int{
		"x-go-type-import":                      0,
		"x-go-type: string":                     2,
		"x-go-type-skip-optional-pointer: true": 1, // labels keeps it; parent_id loses it
		"x-go-type:":                            2, // only the two string hints
	} {
		if got := strings.Count(text, hint); got != want {
			t.Errorf("%q appears %d times, want %d:\n%s", hint, got, want, text)
		}
	}
}
