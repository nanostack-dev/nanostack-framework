package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// relocated maps an upstream path to the path of the same file in Nanostack DB, for the
// files the fork moved. Every comparison and merge uses the upstream path; reading and
// writing our side goes through forkPath.
//
// go-jet's README moved to docs/upstream/README.md so that db/README.md can describe the
// fork. The moved copy is still an upstream file: upstream README changes merge into it.
var relocated = map[string]string{
	"README.md": "docs/upstream/README.md",
}

// forkPath returns where the upstream file p lives in Nanostack DB.
func forkPath(p string) string {
	if moved, ok := relocated[p]; ok {
		return moved
	}
	return p
}

// dropped lists upstream paths (a file, or a directory with a trailing slash) that
// Nanostack DB deletes on purpose because they only work at the root of go-jet's own
// repository: CI and GitHub configuration, and the submodule declaration the testenv
// harness replaced. An upstream change to them is reported as dropped, not as a
// conflict. They still show as deletions in section 2, which the changes table records.
var dropped = []string{
	".circleci/",
	".github/",
	".gitmodules",
	"_config.yml",
}

func isDropped(p string) bool {
	for _, d := range dropped {
		if p == d || (strings.HasSuffix(d, "/") && strings.HasPrefix(p, d)) {
			return true
		}
	}
	return false
}

// ignoredUpstreamFiles returns the upstream files whose Nanostack DB path git ignores in
// dbDir. Such a file exists in a working tree but never reaches a commit, so a checkout
// silently lacks it (go-jet force-adds files its own .gitignore matches). It returns nil
// when dbDir is not inside a git work tree. The test data submodule is skipped: the
// harness replaces it with an ignored symlink on purpose.
func ignoredUpstreamFiles(ctx context.Context, dbDir string, upstreamPaths map[string]bool) ([]string, error) {
	if !insideWorkTree(ctx, dbDir) {
		return nil, nil
	}
	byFork := map[string]string{}
	var stdin bytes.Buffer
	for p := range upstreamPaths {
		if p == testDataPath || excluded(p) || isDropped(p) {
			continue
		}
		fp := forkPath(p)
		byFork[fp] = p
		stdin.WriteString(fp)
		stdin.WriteByte(0)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dbDir, "check-ignore", "--stdin", "-z")
	cmd.Stdin = &stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, nil // nothing ignored
	}
	if err != nil {
		return nil, fmt.Errorf("git check-ignore: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var ignored []string
	for fp := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if fp != "" {
			ignored = append(ignored, fp)
		}
	}
	sort.Strings(ignored)
	return ignored, nil
}

// insideWorkTree reports whether dir is inside a git work tree. Any failure, including a
// missing git, counts as outside.
func insideWorkTree(ctx context.Context, dir string) bool {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// testDataCommitFile holds the jet-test-data commit the testenv harness fetches. It must
// equal the upstream submodule pin of the Base commit and the Test data row of UPSTREAM.md.
const testDataCommitFile = "tests/internal/testenv/testdata.go"

var testDataCommitConst = regexp.MustCompile(`(TestDataCommit\s*=\s*")([0-9a-f]{40})(")`)

// harnessTestDataCommit returns TestDataCommit from testDataCommitFile.
func harnessTestDataCommit(dbDir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dbDir, filepath.FromSlash(testDataCommitFile)))
	if err != nil {
		return "", err
	}
	m := testDataCommitConst.FindSubmatch(content)
	if m == nil {
		return "", fmt.Errorf("%s has no TestDataCommit = \"<40-char sha>\" constant", testDataCommitFile)
	}
	return string(m[2]), nil
}

// setHarnessTestDataCommit rewrites TestDataCommit in testDataCommitFile to sha.
func setHarnessTestDataCommit(dbDir, sha string) error {
	name := filepath.Join(dbDir, filepath.FromSlash(testDataCommitFile))
	content, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if !testDataCommitConst.Match(content) {
		return fmt.Errorf("%s has no TestDataCommit constant", testDataCommitFile)
	}
	content = testDataCommitConst.ReplaceAll(content, []byte("${1}"+sha+"${3}"))
	// G306: a source file, readable like the others. G703: name is under the -db module.
	return os.WriteFile(name, content, 0o644) //nolint:gosec // see above
}

var testDataRow = regexp.MustCompile("(?m)^\\|\\s*Test data\\s*\\|.*`([0-9a-f]{40})`")

// recordedTestData returns the jet-test-data commit of the Test data row of UPSTREAM.md.
func recordedTestData(dbDir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dbDir, "UPSTREAM.md"))
	if err != nil {
		return "", err
	}
	m := testDataRow.FindSubmatch(content)
	if m == nil {
		return "", fmt.Errorf("UPSTREAM.md has no | Test data | ... `<40-char sha>` ... row")
	}
	return string(m[1]), nil
}
