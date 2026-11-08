// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestVersionStringNamesTheFork(t *testing.T) {
	got := versionString()
	if !strings.HasPrefix(got, "Nanostack DB ") ||
		!strings.Contains(got, "fork of go-jet "+version+", commit "+upstreamCommit+")") {
		t.Fatalf("versionString() = %q", got)
	}
}

func TestUpstreamCommitMatchesUPSTREAM(t *testing.T) {
	content, err := os.ReadFile("../../UPSTREAM.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?m)^\\|\\s*Base commit\\s*\\|\\s*`([0-9a-f]{40})`").FindSubmatch(content)
	if m == nil {
		t.Fatal("UPSTREAM.md has no Base commit row")
	}
	if base := string(m[1]); base[:7] != upstreamCommit {
		t.Fatalf(
			"upstreamCommit = %s, the Base commit of UPSTREAM.md is %s: update nanostack_version.go",
			upstreamCommit,
			base,
		)
	}
}
