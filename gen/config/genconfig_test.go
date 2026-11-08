package genconfig

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata")

const fixture = "testdata/app"

func TestGoldenOutput(t *testing.T) {
	files, err := Generate(Options{Dir: fixture})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if *update {
			if err = os.WriteFile(f.Path, f.Data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, readErr := os.ReadFile(f.Path)
		if readErr != nil {
			t.Fatalf("%v; run go -C gen test ./config -update", readErr)
		}
		if !bytes.Equal(want, f.Data) {
			t.Errorf(
				"%s differs from generator output; run go -C gen test ./config -update and review the diff",
				f.Path,
			)
		}
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	first, err := Generate(Options{Dir: fixture})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		again, againErr := Generate(Options{Dir: fixture})
		if againErr != nil {
			t.Fatal(againErr)
		}
		for i := range first {
			if !bytes.Equal(first[i].Data, again[i].Data) {
				t.Fatalf("%s changed between runs", first[i].Path)
			}
		}
	}
}

// TestGeneratedCodeCompilesAndBehaves builds the golden package and runs its
// own tests, which bind real values through the generated Bind.
func TestGeneratedCodeCompilesAndBehaves(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	args := []string{"test", "-count=1", "./" + fixture + "/..."}
	if raceEnabled {
		args = append(args, "-race")
	}
	out, err := exec.CommandContext(t.Context(), "go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go test %s: %v\n%s", fixture, err, out)
	}
}

// TestCheckReportsDriftAndWritesNothing works on a copy of the fixture inside
// the module, because loading the package needs the module's imports.
func TestCheckReportsDriftAndWritesNothing(t *testing.T) {
	// Not t.TempDir: loading the package needs the module's imports, so the
	// copy must live inside the module.
	dir, err := os.MkdirTemp("testdata", "check-") //nolint:usetesting // must sit inside the module

	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	src, err := os.ReadFile(filepath.Join(fixture, "httpconf", "httpconf.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(
		filepath.Join(dir, "config.go"),
		bytes.Replace(src, []byte("package httpconf"), []byte("package check"), 1),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	assertStale(
		t,
		Run(Options{Dir: dir, Check: true}),
		filepath.Join(dir, "config.gen.go"),
		filepath.Join(dir, "CONFIG.md"),
		filepath.Join(dir, ".env.example"),
	)
	if _, err = os.Stat(filepath.Join(dir, "config.gen.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("check mode wrote a file")
	}
	if err = Run(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err = Run(Options{Dir: dir, Check: true}); err != nil {
		t.Fatalf("check after generate: %v", err)
	}

	// A broken previous output must not block regeneration.
	out := filepath.Join(dir, "config.gen.go")
	if err = os.WriteFile(out, []byte("package check\n\nfunc broken( {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStale(t, Run(Options{Dir: dir, Check: true}), out)
	if code := Main([]string{"-dir", dir, "-check"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("Main -check exit code = %d, want 1", code)
	}
	if code := Main([]string{"-dir", dir}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("Main exit code = %d, want 0", code)
	}

	// A file system error names the generator once and the path once.
	doc := filepath.Join(dir, "CONFIG.md")
	if err = os.Remove(doc); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(doc, 0o750); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := Main([]string{"-dir", dir}, &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatalf("Main with a directory at CONFIG.md: exit code = %d, want 1", code)
	}
	if msg := stderr.String(); strings.Count(msg, "nano gen config:") != 1 || strings.Count(msg, doc) != 1 {
		t.Fatalf("Main stderr = %q, want one prefix and one %s", msg, doc)
	}
}

func TestSourceErrorsNameTheFix(t *testing.T) {
	cases := map[string]struct {
		fields string
		want   string
	}{
		"missing tag": {"Name string", `Name: add a config:"name" tag, or config:"-" to skip the field`},
		"bad name":    {"Name string `config:\"Name\"`", `config name "Name" must be lower snake case`},
		"unknown rule": {
			"Port int `config:\"port\" validate:\"gte=1\"`",
			`unknown validate rule "gte"; valid rules: min, max, oneof, scheme`,
		},
		"required rule": {
			"Port int `config:\"port\" validate:\"required\"`",
			"remove validate rule required: settings without a default are required already",
		},
		"rule on wrong type": {
			"Name string `config:\"name\" validate:\"min=1\"`",
			"validate rule min applies to numbers and durations, not string",
		},
		"bad default": {
			"Port int `config:\"port\" default:\"eighty\"`",
			`default "eighty": must be an integer that fits int`,
		},
		"default out of range": {
			"Port int8 `config:\"port\" default:\"300\"`",
			`default "300": must be an integer that fits int8`,
		},
		"default fails rule": {
			"Port int `config:\"port\" default:\"80\" validate:\"min=1024\"`",
			`default "80": is below min=1024`,
		},
		"min above max": {
			"Port int `config:\"port\" validate:\"min=10,max=1\"`",
			"validate min=10 is greater than max=1",
		},
		"secret default": {
			"Key secret.Secret `config:\"key\" default:\"sk_live\"`",
			"secrets cannot have a default",
		},
		"empty default":    {"Name string `config:\"name\" default:\"\"`", `use config:"name,optional"`},
		"unsupported type": {"Ch chan int `config:\"ch\"`", "type chan int is not supported"},
		"env clash": {
			"A string `config:\"a\" env:\"X\"`\n\tB string `config:\"b\" env:\"X\"`",
			"env X is already used by a",
		},
		"bad scheme default": {
			"U *url.URL `config:\"u\" default:\"http://x\" validate:\"scheme=https\"`",
			`http://x does not use scheme https`,
		},
		"oneof bad value": {"N int `config:\"n\" validate:\"oneof=1 two\"`", `validate oneof value "two"`},
		"NaN rule": {
			"R float64 `config:\"r\" validate:\"min=NaN\"`",
			"validate min=NaN: must be a finite number that fits float64",
		},
		"Inf default": {
			"R float64 `config:\"r\" default:\"Inf\"`",
			`default "Inf": must be a finite number that fits float64`,
		},
		"empty list default": {
			"L []string `config:\"l\" default:\",\"`",
			`default "," has no elements, so it counts as unset`,
		},
		"bad option": {"N int `config:\"n,omitempty\"`", `unknown config tag option "omitempty"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, err := os.MkdirTemp("testdata", "bad-") //nolint:usetesting // must sit inside the module
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			src := "package bad\n\nimport (\n\t\"net/url\"\n\n\t\"github.com/nanostack-dev/nanostack-framework/core/secret\"\n)\n\n" +
				"var (\n\t_ url.URL\n\t_ secret.Secret\n)\n\ntype Config struct {\n\t" + tc.fields + "\n}\n"
			if err = os.WriteFile(filepath.Join(dir, "config.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = Generate(Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v\nwant it to contain %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "config.go:") {
				t.Errorf("error %q does not name the file and line", err)
			}
		})
	}
}

func TestDurationLiteral(t *testing.T) {
	for in, want := range map[string]string{"100ms": "100 * time.Millisecond", "1m": "1 * time.Minute", "90s": "90 * time.Second", "1h": "1 * time.Hour", "1500ns": "time.Duration(1500)"} {
		s := &setting{kind: kindDuration}
		if got := literal(s, in); got != want {
			t.Errorf("literal(%s) = %s, want %s", in, got, want)
		}
	}
}

// assertStale fails unless err is a *gen.StaleError that names exactly the
// want paths, in order.
func assertStale(t *testing.T, err error, want ...string) {
	t.Helper()
	stale, ok := errors.AsType[*gen.StaleError](err)
	if !ok || !errors.Is(err, gen.ErrStale) {
		t.Fatalf("got %v, want a *gen.StaleError", err)
	}
	if !slices.Equal(stale.Files, want) {
		t.Fatalf("stale files = %q, want %q", stale.Files, want)
	}
}
