//go:build lintsmoke && unix

// Package smoke runs the pinned golangci-lint on packages that use Go 1.27
// generic methods and fails when it crashes or hangs. It guards against
// crashes and hangs; it does not prove golang/go#81188 fixed, because the
// fixture does not reproduce that bug with the pinned release. Run it with
//
//	go test -tags lintsmoke -count=1 ./lint/smoke/
//
// with the golangci-lint release from lint/golangci-lint-version on PATH, or
// named by GOLANGCI_LINT. See lint/README.md.
package smoke

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/lint"
)

// runTimeout bounds one golangci-lint run. A warm run takes seconds; the
// recursion bugs seen on generic methods never return.
const runTimeout = 3 * time.Minute

// lockMessage is what golangci-lint prints when another instance holds its
// global lock and it may not wait for it.
const lockMessage = "parallel golangci-lint is running"

// crashMarkers match output that means a linter failed, even when
// golangci-lint still exits 0: a panic, a goroutine dump, or a linter that
// could not run.
var crashMarkers = regexp.MustCompile(`(?m)^panic: |^goroutine \d+ |Can't run linter|Running error`)

func TestLintSmoke(t *testing.T) {
	bin := golangciLint(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	runs := []struct {
		name     string
		patterns []string
	}{
		// consumer alone loads listx from export data: the importer side of
		// golang/go#81188, which a source-only run never reaches.
		{name: "importer", patterns: []string{"./lint/smoke/consumer/"}},
		{name: "fixture", patterns: []string{"./lint/smoke/..."}},
		// The same importer run again reads listx's facts from the cache.
		// go-tools#1746 fires only on that warm path; a cold run type-checks
		// listx from source and keeps facts keyed by the real objects.
		{name: "importer-warm", patterns: []string{"./lint/smoke/consumer/"}},
		// The framework's real generic methods, and a package that imports
		// them, under the same crash and hang guard.
		{name: "functional", patterns: []string{"./pkg/functional/..."}},
		{name: "functional-importer", patterns: []string{"./pkg/db/transactor/"}},
	}
	for _, run := range runs {
		t.Run(run.name, func(t *testing.T) {
			lintGuarded(t, root, bin, run.patterns...)
		})
	}

	// Another golangci-lint holds the global lock, as when a developer or an
	// editor lints while the test runs: the run must neither fail nor wait.
	t.Run("lock-held", func(t *testing.T) {
		holdLintLock(t)
		lintGuarded(t, root, bin, "./lint/smoke/consumer/")
	})
}

// holdLintLock takes golangci-lint's global file lock, the way another
// golangci-lint process holds it, until t ends. When another process already
// holds it, that process stands in.
func holdLintLock(t *testing.T) {
	t.Helper()
	tmp := os.TempDir() //nolint:usetesting // golangci-lint's lock lives here
	file, err := os.OpenFile(filepath.Join(tmp, "golangci-lint.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() }) // closing releases the lock
	if lockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		if errors.Is(lockErr, syscall.EWOULDBLOCK) {
			t.Log("another golangci-lint already holds the lock")
			return
		}
		t.Fatal(lockErr)
	}
}

// lintGuarded runs golangci-lint on patterns in root and fails on a hang, a
// crash marker, another instance's lock, and a non-zero exit.
//
// --allow-parallel-runners skips golangci-lint's global lock. Without it a
// run exits when another golangci-lint holds the lock, which this test would
// report as a lint failure; with --allow-serial-runners it waits instead, and
// an editor's lint that holds the lock for minutes would use up runTimeout
// and be reported as a hang.
func lintGuarded(t *testing.T, root, bin string, patterns ...string) {
	t.Helper()
	args := append([]string{"run", "--allow-parallel-runners"}, patterns...)
	output, err := runGuarded(t, root, bin, args...)
	if errors.Is(err, errHung) {
		t.Errorf("golangci-lint %s %v; goroutine dump:\n%s",
			strings.Join(args, " "), err, output)
		return
	}
	if marker := crashMarkers.FindString(output); marker != "" {
		t.Errorf("golangci-lint crashed (%q in output):\n%s", marker, output)
		return
	}
	if strings.Contains(strings.ToLower(output), lockMessage) {
		t.Errorf("golangci-lint %s did not get the global lock that another golangci-lint holds; "+
			"this is not a lint failure:\n%s", strings.Join(patterns, " "), output)
		return
	}
	if err != nil {
		t.Errorf("golangci-lint run %s: %v\n%s", strings.Join(patterns, " "), err, output)
	}
}

// golangciLint returns the golangci-lint binary and fails unless it is the
// release the standard pins.
func golangciLint(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("GOLANGCI_LINT")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("golangci-lint"); err != nil {
			t.Fatalf("golangci-lint %s is not on PATH; install it or set GOLANGCI_LINT", lint.Version())
		}
	}
	output, err := runGuarded(t, ".", bin, "version", "--short")
	if err != nil {
		t.Fatalf("%s version: %v\n%s", bin, err, output)
	}
	if got, want := "v"+strings.TrimSpace(output), lint.Version(); got != want {
		t.Fatalf("%s is golangci-lint %s; the standard pins %s (lint/golangci-lint-version)", bin, got, want)
	}
	return bin
}

// errHung marks a run that runGuarded stopped at runTimeout.
var errHung = fmt.Errorf("hung for %s", runTimeout)

// runGuarded runs bin under runTimeout. On timeout it sends SIGQUIT, so a Go
// program prints every goroutine before it exits, and returns errHung with
// that dump as the output.
func runGuarded(t *testing.T, dir, bin string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()

	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGQUIT) }
	cmd.WaitDelay = 15 * time.Second // then SIGKILL

	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output.String(), errHung
	}
	return output.String(), err
}
