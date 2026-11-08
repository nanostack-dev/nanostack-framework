package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fresh := []string{"gen", "wire", "-dir", "../../wire/testdata/signatures", "-check"}
	if code := run(fresh, &stdout, &stderr); code != 0 {
		t.Fatalf("check on fresh testdata: exit %d\n%s", code, stderr.String())
	}

	stderr.Reset()
	cyclic := []string{"gen", "wire", "-dir", "../../wire/testdata/cycle", "-check"}
	if code := run(cyclic, &stdout, &stderr); code != 1 {
		t.Fatalf("check on a cyclic graph: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "dependency cycle") || !strings.Contains(stderr.String(), "\n  fix: ") {
		t.Fatalf("stderr = %q, want the cycle diagnostic and its fix", stderr.String())
	}

	// An output path the generator cannot read is the machine's problem,
	// not the source's: exit 3. httpconf is a directory in the fixture.
	stderr.Reset()
	unreadable := []string{"gen", "config", "-dir", "../../config/testdata/app", "-doc", "httpconf", "-check"}
	if code := run(unreadable, &stdout, &stderr); code != 3 || !strings.Contains(stderr.String(), "fix: ") {
		t.Fatalf("check with a directory as -doc: exit %d, stderr %q; want 3 and a fix", code, stderr.String())
	}

	for _, args := range [][]string{{}, {"gen", "nope"}, {"build", "wire"}} {
		stderr.Reset()
		code := run(args, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "usage: nano gen [flags]\n") ||
			!strings.Contains(stderr.String(), "nano gen {config,errors,validate,wire} [flags]") {
			t.Fatalf("run(%q): exit %d stderr %q, want usage and exit 2", args, code, stderr.String())
		}
	}
}

// TestPipelineIsRouted runs `nano gen` without a generator name: the
// pipeline, on its committed fixture module.
func TestPipelineIsRouted(t *testing.T) {
	const fixture = "../../pipeline/testdata/app"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"gen", "-check", "-dir", fixture}, &stdout, &stderr); code != 0 ||
		stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("nano gen -check: exit %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	if code := run([]string{"gen", "-list", "-dir", fixture}, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "validate  api\n  in   api/openapi.yaml\n") ||
		!strings.Contains(stdout.String(), "wire      cmd/app\n") {
		t.Fatalf("nano gen -list: exit %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	stderr.Reset()
	if code := run([]string{"gen", "-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "-only") {
		t.Fatalf("nano gen -h: exit %d, stderr %q; want the flags and exit 0", code, stderr.String())
	}
}

// TestEveryGeneratorIsRouted runs each generator's -check on its committed
// testdata through the one nano command.
func TestEveryGeneratorIsRouted(t *testing.T) {
	const golden = "../../validate/internal/golden/required"
	for name, args := range map[string][]string{
		"config":   {"gen", "config", "-dir", "../../config/testdata/app", "-check"},
		"errors":   {"gen", "errors", "-dir", "../../errors/testdata/billing", "-check"},
		"validate": {"gen", "validate", "-spec", golden + "/openapi.yaml", "-dir", golden, "-check"},
		"wire":     {"gen", "wire", "-dir", "../../wire/testdata/example", "-out", "internal/gen/wire", "-check"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("nano %s: exit %d\n%s", strings.Join(args, " "), code, stderr.String())
			}
			// A help request is not a usage error: it prints the flags and
			// exits 0. A bad flag still exits 2.
			stderr.Reset()
			if code := run([]string{"gen", name, "-h"}, &stdout, &stderr); code != 0 ||
				!strings.Contains(stderr.String(), "-check") {
				t.Fatalf("nano gen %s -h: exit %d, stderr %q; want the flags and exit 0", name, code, stderr.String())
			}
			if code := run([]string{"gen", name, "-no-such-flag"}, &stdout, &stderr); code != 2 {
				t.Fatalf("nano gen %s -no-such-flag: exit %d, want 2", name, code)
			}
		})
	}
}
