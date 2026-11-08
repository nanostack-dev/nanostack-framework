package genvalidate

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowCoversALocationAndItsSubtree(t *testing.T) {
	d := Diagnostic{Loc: "#/components/schemas/Body/properties/shape", Message: "oneOf/anyOf is not enforced"}
	cases := []struct {
		allow []string
		want  bool
	}{
		{nil, false},
		{[]string{"#/components/schemas/Body/properties/shape"}, true},
		{[]string{"#/components/schemas/Body"}, true},
		{[]string{"#/components/schemas/Body/"}, true},
		{[]string{"#/components/schemas/Bo"}, false},
		{[]string{"#/components/schemas/Other", "#/components/schemas/Body"}, true},
	}
	for _, c := range cases {
		if got := allowed(d, c.allow); got != c.want {
			t.Errorf("allowed(%v) = %v, want %v", c.allow, got, c.want)
		}
	}
}

func TestMainRejectsAnAllowThatIsNotAPointer(t *testing.T) {
	var stderr bytes.Buffer
	if code := Main([]string{"-spec", "x.yaml", "-allow", "Body"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("exit = %d, stderr %s", code, stderr.String())
	}
}

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

// Strict is the default: an unenforced feature fails the run until its
// location is acknowledged.
func TestMainFailsOnUnenforcedFeatures(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	src, err := os.ReadFile(filepath.Join(dir, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(src), "note: {type: string, maxLength: 3}",
		"note: {type: string, maxLength: 3, not: {const: x}}", 1)
	if edited == string(src) {
		t.Fatal("the golden spec changed; update this test")
	}
	spec := filepath.Join(t.TempDir(), "openapi.yaml")
	if err = os.WriteFile(spec, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	const failure = "schema features are not enforced: 1"
	const listed = "not enforced: #/components/schemas/Body/properties/note: not is not enforced"

	var stderr bytes.Buffer
	if code := Main([]string{"-spec", spec, "-dir", dir, "-check"}, io.Discard, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), listed) ||
		!strings.Contains(stderr.String(), failure) {
		t.Fatalf("exit = %d, stderr %s", code, stderr.String())
	}

	stderr.Reset()
	Main([]string{"-spec", spec, "-dir", dir, "-check", "-allow", "#/components/schemas/Body"}, io.Discard, &stderr)
	if strings.Contains(stderr.String(), failure) || !strings.Contains(stderr.String(), "not enforced (allowed):") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
