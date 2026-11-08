package testenv

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// DiffDirs compares every file under want and got, and returns a readable report, or ""
// when the trees are identical.
func DiffDirs(want, got string) (string, error) {
	w, err := readTree(want)
	if err != nil {
		return "", err
	}
	g, err := readTree(got)
	if err != nil {
		return "", err
	}
	names := map[string]bool{}
	for name := range w {
		names[name] = true
	}
	for name := range g {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	var report strings.Builder
	for _, name := range sorted {
		wb, inW := w[name]
		gb, inG := g[name]
		switch {
		case !inG:
			fmt.Fprintf(&report, "  only in %s: %s\n", want, name)
		case !inW:
			fmt.Fprintf(&report, "  only in %s: %s\n", got, name)
		case !bytes.Equal(wb, gb):
			fmt.Fprintf(&report, "  differs: %s\n", name)
		}
	}
	return report.String(), nil
}

// RequireSameTree fails t when the trees under want and got differ.
func RequireSameTree(t *testing.T, want, got string) {
	t.Helper()
	report, err := DiffDirs(want, got)
	if err != nil {
		t.Fatal(err)
	}
	if report != "" {
		t.Fatalf("generated trees differ:\n%s", report)
	}
	if files, _ := readTree(want); len(files) == 0 {
		t.Fatalf("nothing was generated under %s", want)
	}
}

// RunJet runs the jet CLI the harness installed on PATH and fails t when it fails.
func RunJet(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "jet", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("jet %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// RequireJetRejectsNullable checks that the CLI rejects an invalid -nullable value with
// exit status 2 and a message that lists the choices.
func RequireJetRejectsNullable(t *testing.T, args ...string) {
	t.Helper()
	args = append(args, "-nullable=maybe")
	out, err := exec.CommandContext(t.Context(), "jet", args...).
		CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("jet -nullable=maybe: want exit status 2, got %v\n%s", err, out)
	}
	if !strings.Contains(
		string(out),
		`-nullable="maybe" is not valid; use pointer (*T) or option (functional.Option[T])`,
	) {
		t.Fatalf("jet -nullable=maybe: unexpected output:\n%s", out)
	}
}
