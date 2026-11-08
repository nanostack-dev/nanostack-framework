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
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
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
	if code := Main([]string{"-dir", dir}, &bytes.Buffer{}, &stderr); code != 3 {
		t.Fatalf("Main with a directory at CONFIG.md: exit code = %d, want 3 (environment)", code)
	}
	if msg := stderr.String(); strings.Count(msg, doc) != 1 || !strings.Contains(msg, "\n  fix: ") {
		t.Fatalf("Main stderr = %q, want the path once and a fix", msg)
	}
}

// TestSourceErrorsNameTheFix is the table of the generator's error paths:
// each one is a *gen.Error at the field's file and line, with a fix.
func TestSourceErrorsNameTheFix(t *testing.T) {
	cases := map[string]struct {
		fields string
		want   string
	}{
		"missing tag":      {"Name string", `Name: the field has no config tag`},
		"unexported field": {"name string `config:\"name\"`", "name: unexported fields cannot be bound"},
		"bad name":         {"Name string `config:\"Name\"`", `config name "Name" must be lower snake case`},
		"declared twice": {
			"A struct{ X string `config:\"x\"` } `config:\"a\"`\n\tB string `config:\"a\"`",
			"setting a is declared twice",
		},
		"bad env": {"Name string `config:\"name\" env:\"name\"`", `env "name" must be upper snake case`},
		"default on a nested struct": {
			"A struct{ X string `config:\"x\"` } `config:\"a\" default:\"x\"`",
			"A: default does not apply to a nested struct",
		},
		"optional nested struct": {
			"A struct{ X string `config:\"x\"` } `config:\"a,optional\"`",
			"A: optional does not apply to a nested struct",
		},
		"optional with a default": {
			"Name string `config:\"name,optional\" default:\"x\"`",
			"a setting with a default is never empty",
		},
		"unknown rule": {"Port int `config:\"port\" validate:\"gte=1\"`", `unknown validate rule "gte"`},
		"rule set twice": {
			"Port int `config:\"port\" validate:\"min=1,min=2\"`",
			"validate rule min is set twice",
		},
		"required rule": {
			"Port int `config:\"port\" validate:\"required\"`",
			"validate rule required has no effect",
		},
		"rule on wrong type": {
			"Name string `config:\"name\" validate:\"min=1\"`",
			"validate rule min applies to numbers and durations, not string",
		},
		"bad rule argument": {
			"Port int `config:\"port\" validate:\"min=one\"`",
			"validate min=one: must be an integer",
		},
		"oneof on wrong type": {
			"On bool `config:\"on\" validate:\"oneof=true\"`",
			"validate rule oneof applies to strings and integers, not bool",
		},
		"empty oneof": {"Mode string `config:\"mode\" validate:\"oneof=\"`", "validate oneof has no values"},
		"scheme on wrong type": {
			"Name string `config:\"name\" validate:\"scheme=https\"`",
			"validate rule scheme applies to *url.URL, not string",
		},
		"empty scheme": {"U *url.URL `config:\"u\" validate:\"scheme=\"`", "validate scheme has no schemes"},
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
		"empty default":    {"Name string `config:\"name\" default:\"\"`", `default:"" is not a default`},
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
		"bad option":  {"N int `config:\"n,omitempty\"`", `unknown config tag option "omitempty"`},
		"no settings": {"", "Config has no settings"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "package bad\n\nimport (\n\t\"net/url\"\n\n\t\"github.com/nanostack-dev/nanostack-framework/core/secret\"\n)\n\n" +
				"var (\n\t_ url.URL\n\t_ secret.Secret\n)\n\ntype Config struct {\n\t" + tc.fields + "\n}\n"
			_, err := Generate(Options{Dir: badPackage(t, src)})
			problems := gentest.Problems(t, err)
			if !slices.ContainsFunc(problems, func(p *gen.Error) bool {
				return strings.Contains(p.Msg, tc.want) && p.File == "config.go" && p.Line > 0
			}) {
				t.Fatalf("got error %v\nwant a problem at config.go:<line> whose message contains %q", err, tc.want)
			}
		})
	}
}

// TestPackageErrorsNameTheFix covers the errors about the package as a whole.
func TestPackageErrorsNameTheFix(t *testing.T) {
	cases := map[string]struct {
		src  string
		opts func(dir string) Options
		want string
		env  bool
	}{
		"no such type": {
			src:  "package bad\n\ntype Settings struct{}\n",
			want: "has no type Config",
		},
		"not a struct": {
			src:  "package bad\n\ntype Config int\n",
			want: "Config must be a struct type",
		},
		"does not compile": {
			src:  "package bad\n\ntype Config struct{ A string `config:\"a\"` }\n\nvar _ = undefined\n",
			want: "undefined: undefined",
		},
		"no Go files": {
			src:  "",
			want: "no Go files besides config.gen.go",
		},
		"no such directory": {
			opts: func(dir string) Options { return Options{Dir: filepath.Join(dir, "missing")} },
			want: "the directory does not exist",
		},
		"relative overlay path": {
			src: "package bad\n\ntype Config struct{ A string `config:\"a\"` }\n",
			opts: func(dir string) Options {
				return Options{Dir: dir, Overlay: map[string][]byte{"config.go": nil}}
			},
			want: "overlay path config.go is not absolute",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := badPackage(t, tc.src)
			opts := Options{Dir: dir}
			if tc.opts != nil {
				opts = tc.opts(dir)
			}
			_, err := Generate(opts)
			problems := gentest.Problems(t, err)
			if !slices.ContainsFunc(problems, func(p *gen.Error) bool { return strings.Contains(p.Msg, tc.want) }) {
				t.Fatalf("got error %v\nwant a problem whose message contains %q", err, tc.want)
			}
			if gentest.Environment(err) != tc.env {
				t.Fatalf("environment = %v, want %v: %v", !tc.env, tc.env, err)
			}
		})
	}
}

// badPackage writes src as config.go in a new package inside the module,
// where loading it resolves the module's imports. An empty src writes no file.
func badPackage(t *testing.T, src string) string {
	t.Helper()
	dir, err := os.MkdirTemp("testdata", "bad-") //nolint:usetesting // must sit inside the module
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if src != "" {
		if err = os.WriteFile(filepath.Join(dir, "config.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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

// TestRenamedFieldDoesNotDeadlockWire is the config half of the config/wire
// deadlock. The fixture's wire.gen.go reads Config.Postgres, which was renamed
// to Database, so the package does not build. config loads with
// -tags=nanogen, which drops wire.gen.go and run.go, and regenerates.
func TestRenamedFieldDoesNotDeadlockWire(t *testing.T) {
	const dir = "testdata/deadlock"
	if testing.Short() {
		t.Skip("runs the go command")
	}
	out, err := exec.CommandContext(t.Context(), "go", "build", "./"+dir).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "cfg.Postgres undefined") {
		t.Fatalf("go build %s = %v\n%s\nwant the stale wire.gen.go to break the build", dir, err, out)
	}
	files, err := Generate(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Contains(files[0].Data, []byte("Database")) || bytes.Contains(files[0].Data, []byte("Postgres")) {
		t.Fatalf("config.gen.go does not bind the renamed field:\n%s", files[0].Data)
	}
}

// TestOverlayReplacesTheDisk generates from overlaid source, as a -check run
// of the pipeline does with the fresh outputs of earlier steps. The output
// file stays the generator's own stub whatever the overlay holds.
func TestOverlayReplacesTheDisk(t *testing.T) {
	dir := badPackage(t, "package overlay\n\ntype Config struct {\n\tOnDisk string `config:\"on_disk\"`\n}\n")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(Options{Dir: dir, Overlay: map[string][]byte{
		filepath.Join(abs, "config.go"): []byte(
			"package overlay\n\ntype Config struct {\n\tOverlaid string `config:\"overlaid\"`\n}\n"),
		filepath.Join(abs, "config.gen.go"): []byte("package overlay\n\nfunc broken( {\n"),
	}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Contains(files[0].Data, []byte("Overlaid")) || bytes.Contains(files[0].Data, []byte("OnDisk")) {
		t.Fatalf("config.gen.go was not generated from the overlay:\n%s", files[0].Data)
	}
}
