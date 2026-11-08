package main

import (
	"regexp"
	"slices"
	"testing"
)

// The patterns match every upstream file and no Nanostack file. A tree without a Nanostack
// file collapses to one pattern; a directory with both kinds lists its upstream files.
func TestPatternsMatchExactlyTheUpstreamFiles(t *testing.T) {
	upstream := []string{
		"doc.go", "qrm/qrm.go", "qrm/qrm_test.go", "qrm/internal/null_types.go",
		"generator/mysql/gen.go", "generator/metadata/meta.go", "generator/template/process.go",
		"internal/utils/is/is.go", "internal/utils/ptr/ptr.go",
	}
	nanostack := []string{"version.go", "qrm/option.go", "generator/template/nullable.go", "query/query.go"}

	got := patterns(upstream, nanostack)

	want := []string{
		`^(doc)\.go$`,
		`^generator/metadata/`,
		`^generator/mysql/`,
		`^generator/template/(process)\.go$`,
		`^internal/`,
		`^qrm/(qrm|qrm_test)\.go$`,
		`^qrm/internal/`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("patterns:\n got  %q\n want %q", got, want)
	}
	matches := func(file string) bool {
		return slices.ContainsFunc(got, func(p string) bool { return regexp.MustCompile(p).MatchString(file) })
	}
	for _, f := range upstream {
		if !matches(f) {
			t.Errorf("upstream file %s is not excluded", f)
		}
	}
	for _, f := range nanostack {
		if matches(f) {
			t.Errorf("Nanostack file %s is excluded", f)
		}
	}
}

func TestReplaceBlockKeepsTheMarkers(t *testing.T) {
	config := []byte("formatters:\n  exclusions:\n    # BEGIN upstreamfmt\n    paths:\n      - ^old/\n" +
		"    # END upstreamfmt\n  settings: {}\n")

	got, err := replaceBlock(config, render([]string{"^new/"}))

	want := "formatters:\n  exclusions:\n    # BEGIN upstreamfmt\n    paths:\n      - ^new/\n" +
		"    # END upstreamfmt\n  settings: {}\n"
	if err != nil || string(got) != want {
		t.Fatalf("replaceBlock = (%q, %v), want %q", got, err, want)
	}
}
