package genvalidate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
)

// A spec with no unenforced feature passes, with no -strict flag to opt in.
func TestMainPassesWithoutDiagnostics(t *testing.T) {
	var stderr bytes.Buffer
	dir := filepath.Join(goldenRoot, "required")
	if code := Main([]string{"-spec", dir + "/openapi.yaml", "-dir", dir, "-check"}, io.Discard, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "not enforced") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

// editedSpec writes the required golden spec with each old, new pair
// replaced to a temporary file and returns its path.
func editedSpec(t *testing.T, pairs ...string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(goldenRoot, "required", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := string(src)
	for i := 0; i+1 < len(pairs); i += 2 {
		next := strings.Replace(edited, pairs[i], pairs[i+1], 1)
		if next == edited {
			t.Fatalf("the golden spec has no %q; update this test", pairs[i])
		}
		edited = next
	}
	spec := filepath.Join(t.TempDir(), "openapi.yaml")
	if err = os.WriteFile(spec, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	return spec
}

// Strict is the default: an unenforced feature fails the run, at the line
// of its schema, until an x-nano-unenforced extension accepts it.
func TestMainFailsOnUnenforcedFeatures(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	spec := editedSpec(t, "note: {type: string, maxLength: 3}",
		"note: {type: string, maxLength: 3, not: {const: x}}")
	const failure = `fix: change the schema, or accept the gap with x-nano-unenforced: "<reason>" ` +
		"on the schema at #/components/schemas/Body/properties/note"
	const listed = "openapi.yaml:26: not enforced: #/components/schemas/Body/properties/note: not is not enforced"

	var stderr bytes.Buffer
	if code := Main([]string{"-spec", spec, "-dir", dir, "-check"}, io.Discard, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), listed) ||
		!strings.Contains(stderr.String(), failure) {
		t.Fatalf("exit = %d, stderr %s", code, stderr.String())
	}
}

// An extension on the schema or on one it is nested in accepts the gap.
func TestWaiverCoversTheSchemaAndItsSubtree(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	for name, spec := range map[string]string{
		"own": editedSpec(t, "note: {type: string, maxLength: 3}",
			`note: {type: string, maxLength: 3, not: {const: x}, x-nano-unenforced: "checked by the handler"}`),
		"ancestor": editedSpec(t,
			"    Body:\n      type: object", "    Body:\n      x-nano-unenforced: \"checked by the handler\"\n      type: object",
			"note: {type: string, maxLength: 3}", "note: {type: string, maxLength: 3, not: {const: x}}"),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := Generate(Options{Spec: spec, Dir: dir})
			if err != nil {
				t.Fatal(err)
			}
			if err = res.Gaps(); err != nil {
				t.Fatalf("Gaps() = %v, want none", err)
			}
		})
	}
}

// A waiver that accepts nothing fails, so it cannot hide the next gap, and a
// waiver needs a reason.
func TestWaiverProblems(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	spec := editedSpec(t, "note: {type: string, maxLength: 3}",
		`note: {type: string, maxLength: 3, x-nano-unenforced: "nothing to accept"}`)
	res, err := Generate(Options{Spec: spec, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	problems := gentest.Problems(t, res.Gaps())
	if len(problems) != 1 || problems[0].Line != 26 ||
		problems[0].Msg != "#/components/schemas/Body/properties/note: x-nano-unenforced accepts no gap" {
		t.Fatalf("problems = %v", problems)
	}

	spec = editedSpec(t, "note: {type: string, maxLength: 3}", `note: {type: string, x-nano-unenforced: ""}`)
	_, err = Generate(Options{Spec: spec, Dir: dir})
	problems = gentest.Problems(t, err)
	if len(problems) != 1 || !strings.Contains(problems[0].Msg, "x-nano-unenforced needs the reason") {
		t.Fatalf("problems = %v", problems)
	}
}
