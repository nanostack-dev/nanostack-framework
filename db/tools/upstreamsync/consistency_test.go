package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The jet-test-data commit appears in three places of the real module. The harness
// fetches TestDataCommit; the other two document it. They must agree, or the docs claim
// test data the suites never use.
func TestTestDataPinIsConsistent(t *testing.T) {
	dbDir := filepath.Join("..", "..")
	harness, err := harnessTestDataCommit(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := recordedTestData(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	if recorded != harness {
		t.Errorf("UPSTREAM.md Test data row pins %s, TestDataCommit in %s is %s", recorded, testDataCommitFile, harness)
	}
	readme, err := os.ReadFile(filepath.Join(dbDir, "tests", "Readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "`"+harness+"`") {
		t.Errorf("tests/Readme.md does not name TestDataCommit %s", harness)
	}
}

// A blank line inside a Markdown table ends it, and every row after it renders as stray
// pipe text. The changes table must be one block.
func TestChangesTableIsOneBlock(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "UPSTREAM.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	start := strings.Index(text, "## Changes from upstream")
	if start < 0 {
		t.Fatal("UPSTREAM.md has no Changes from upstream section")
	}
	section := text[start:]
	if end := strings.Index(section[1:], "\n## "); end >= 0 {
		section = section[:end+1]
	}
	inTable, blocks, rows := false, 0, 0
	for line := range strings.SplitSeq(section, "\n") {
		isRow := strings.HasPrefix(line, "|")
		if isRow && !inTable {
			blocks++
		}
		if isRow {
			rows++
		}
		inTable = isRow
	}
	if blocks != 1 {
		t.Fatalf("the changes table is split into %d blocks; remove the blank lines between its rows", blocks)
	}
	if rows < 3 {
		t.Fatalf("the changes table has %d lines", rows)
	}
}
