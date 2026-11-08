package genvalidate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
)

const goldenRoot = "internal/golden"

func goldenDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(goldenRoot)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(goldenRoot, e.Name()))
		}
	}
	if len(dirs) < 13 {
		t.Fatalf("found %d golden packages", len(dirs))
	}
	return dirs
}

// TestGolden regenerates every golden package and compares it with the
// checked-in validate.gen.go. One package per keyword family; update them
// with go -C gen generate ./validate/internal/golden/...
func TestGolden(t *testing.T) {
	for _, dir := range goldenDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			t.Parallel()
			cfg := Options{Spec: filepath.Join(dir, "openapi.yaml"), Dir: dir}
			gp := loaded(t, dir)
			res, err := generateWith(cfg, gp)
			if err != nil {
				t.Fatal(err)
			}
			have, err := os.ReadFile(res.File.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(have, res.File.Data) {
				t.Fatalf("%s is stale; run go generate ./%s", res.File.Path, dir)
			}
			again, err := generateWith(cfg, gp)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(res.File.Data, again.File.Data) {
				t.Fatal("generation is not deterministic")
			}
		})
	}
}

type loadResult struct {
	once sync.Once
	gp   *goPackage
	err  error
}

var loadedGo sync.Map // dir → *loadResult

// loaded caches type-checked golden packages: loading costs seconds.
func loaded(t *testing.T, dir string) *goPackage {
	t.Helper()
	v, _ := loadedGo.LoadOrStore(dir, &loadResult{})
	lr, ok := v.(*loadResult)
	if !ok {
		t.Fatalf("loadedGo holds %T", v)
	}
	lr.once.Do(func() { lr.gp, lr.err = loadPackage(Options{Dir: dir, Output: DefaultOutput}) })
	if lr.err != nil {
		t.Fatal(lr.err)
	}
	return lr.gp
}

func TestCheckReportsMissingFile(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	err := Run(Options{Spec: filepath.Join(dir, "openapi.yaml"), Dir: dir, Output: "missing.gen.go", Check: true})
	if !errors.Is(err, gen.ErrStale) {
		t.Fatalf("Run with Check = %v; want gen.ErrStale", err)
	}
}

func TestDiagnosticsAreListedInTheFile(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	spec := writeSpec(t, dir, func(s string) string {
		return strings.Replace(
			s,
			"note: {type: string, maxLength: 3}",
			"note: {type: string, maxLength: 3, format: color, not: {const: x}}",
			1,
		)
	})
	res, err := generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"#/components/schemas/Body/properties/note: format color is not checked",
		"#/components/schemas/Body/properties/note: not is not enforced",
	}
	if len(res.Diagnostics) != len(want) {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
	for i, d := range res.Diagnostics {
		if d.String() != want[i] {
			t.Errorf("diagnostic %d = %q, want %q", i, d, want[i])
		}
		if !bytes.Contains(res.File.Data, []byte(want[i])) {
			t.Errorf("generated file does not list %q", want[i])
		}
	}
}

func TestGenerationErrors(t *testing.T) {
	dir := filepath.Join(goldenRoot, "required")
	cases := map[string]struct {
		edit func(string) string
		want string
	}{
		"lookahead pattern": {
			func(s string) string {
				return strings.Replace(s, "name: {type: string}", "name: {type: string, pattern: '^(?=a).*$'}", 1)
			},
			"lookaround assertions are not supported by RE2",
		},
		"backreference": {
			func(s string) string {
				return strings.Replace(s, "name: {type: string}", `name: {type: string, pattern: '(a)\1'}`, 1)
			},
			"backreferences are not supported by RE2",
		},
		"property without a Go field": {
			func(s string) string {
				return strings.Replace(
					s,
					"name: {type: string}",
					"name: {type: string}\n        ghost: {type: string}",
					1,
				)
			},
			`property "ghost" has no field in Go type`,
		},
		"operation missing from the spec": {
			func(s string) string { return strings.Replace(s, "/check:", "/other:", 1) },
			"StrictServerInterface.Check: no operation POST /check in the spec",
		},
		"external reference": {
			func(s string) string {
				return strings.Replace(s, "$ref: '#/components/schemas/Body'", "$ref: 'other.yaml#/Body'", 1)
			},
			`unsupported reference "other.yaml#/Body"`,
		},
		"unresolved reference": {
			func(s string) string {
				return strings.Replace(s, "'#/components/schemas/Body'", "'#/components/schemas/Nope'", 1)
			},
			`unresolved reference "#/components/schemas/Nope"`,
		},
		"negative length": {
			func(s string) string { return strings.Replace(s, "maxLength: 3", "maxLength: -3", 1) },
			`want a non-negative integer, got "-3"`,
		},
		"bad number": {
			func(s string) string { return strings.Replace(s, "minimum: 0", "minimum: zero", 1) },
			`want a number, got "zero"`,
		},
		"tagged boolean": {
			func(s string) string {
				return strings.Replace(
					s,
					"enabled: {type: boolean}",
					"enabled: {type: boolean, enum: [!!bool maybe]}",
					1,
				)
			},
			`"maybe" is not a boolean`,
		},
		"tagged number": {
			func(s string) string {
				return strings.Replace(
					s,
					"count: {type: integer, minimum: 0}",
					"count: {type: integer, enum: [!!int x]}",
					1,
				)
			},
			`"x" is not a number`,
		},
		"schema that is not an object": {
			func(s string) string { return strings.Replace(s, "name: {type: string}", "name: [string]", 1) },
			"schema is not an object",
		},
		"unsupported version": {
			func(s string) string { return strings.Replace(s, "openapi: 3.0.3", "openapi: 2.0.0", 1) },
			`unsupported openapi version "2.0.0"`,
		},
		"not YAML": {
			func(s string) string { return strings.Replace(s, "paths:", "paths: [", 1) },
			"yaml:",
		},
		"not an OpenAPI document": {
			func(string) string { return "- a\n- b\n" },
			"not an OpenAPI document",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			spec := writeSpec(t, dir, c.edit)
			_, err := generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir))
			gentest.Problems(t, err)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// TestPackageErrorsNameTheFix covers the errors about the Go package and the
// spec file rather than a schema.
func TestPackageErrorsNameTheFix(t *testing.T) {
	golden := filepath.Join(goldenRoot, "required")
	cases := map[string]struct {
		src  string // api.go of a new package; empty for the golden package
		opts func(dir string) Options
		want string
	}{
		"no strict server": {src: "package api\n\ntype Body struct{}\n", want: "has no StrictServerInterface"},
		"strict server is not an interface": {
			src:  "package api\n\ntype StrictServerInterface struct{}\n",
			want: "StrictServerInterface is not an interface",
		},
		"operation without a path comment": {
			src:  "package api\n\ntype StrictServerInterface interface {\n\t// Check checks.\n\tCheck()\n}\n",
			want: "StrictServerInterface.Check has no (METHOD /path) comment",
		},
		"unexpected signature": {
			src:  "package api\n\ntype StrictServerInterface interface {\n\t// (POST /check)\n\tCheck()\n}\n",
			want: "StrictServerInterface.Check: unexpected signature",
		},
		"syntax error": {src: "package api\n\nfunc broken( {\n", want: "expected"},
		"no Go files":  {src: "-", want: "no Go files besides validate.gen.go"},
		"missing spec": {
			opts: func(string) Options { return Options{Spec: "nope.yaml", Dir: golden} },
			want: "the file does not exist",
		},
		"relative overlay path": {
			opts: func(string) Options {
				return Options{
					Spec:    golden + "/openapi.yaml",
					Dir:     golden,
					Overlay: map[string][]byte{"api.gen.go": nil},
				}
			},
			want: "overlay path api.gen.go is not absolute",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := golden
			if c.src != "" {
				dir = modulePackage(t, c.src)
			}
			opts := Options{Spec: filepath.Join(golden, "openapi.yaml"), Dir: dir}
			if c.opts != nil {
				opts = c.opts(dir)
			}
			_, err := Generate(opts)
			gentest.Problems(t, err)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// TestOverlayReplacesTheDisk generates from an overlaid Go package, as a
// -check run of the pipeline does with the fresh output of the api step.
// The output file stays the generator's own, whatever the overlay holds.
func TestOverlayReplacesTheDisk(t *testing.T) {
	golden := filepath.Join(goldenRoot, "required")
	dir := modulePackage(t, "package required\n")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	api, err := os.ReadFile(filepath.Join(golden, "api.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Generate(Options{Spec: filepath.Join(golden, "openapi.yaml"), Dir: dir, Overlay: map[string][]byte{
		filepath.Join(abs, "api.go"):          api,
		filepath.Join(abs, "validate.gen.go"): []byte("package api\n\nfunc broken( {\n"),
	}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want, err := os.ReadFile(filepath.Join(golden, "validate.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The header names the spec relative to the package, so skip it.
	_, got, _ := bytes.Cut(res.File.Data, []byte("\n"))
	_, want, _ = bytes.Cut(want, []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("validate.gen.go from the overlay differs from the golden one:\n%s", res.File.Data)
	}
}

// modulePackage writes src as api.go in a new package inside the module,
// where loading it resolves the module's imports. src "-" writes no file.
func modulePackage(t *testing.T, src string) string {
	t.Helper()
	if err := os.MkdirAll("testdata", 0o750); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("testdata", "pkg-") //nolint:usetesting // must sit inside the module
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
		_ = os.Remove("testdata") // only when no other test still uses it
	})
	if src != "-" {
		if err = os.WriteFile(filepath.Join(dir, "api.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeSpec(t *testing.T, dir string, edit func(string) string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	edited := edit(string(data))
	if edited == string(data) {
		t.Fatal("edit did not apply")
	}
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if writeErr := os.WriteFile(path, []byte(edited), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	return path
}

func TestTranslatePattern(t *testing.T) {
	cases := []struct{ in, want string }{
		{`^[a-z]+$`, `^[a-z]+$`},
		{`.*\S.*`, `[^\n\r\x{2028}\x{2029}]*[^` + ecmaSpace + `][^\n\r\x{2028}\x{2029}]*`},
		{"^\\u0041[\\s_]$", `^\x{41}[` + ecmaSpace + `_]$`},
		{`a\/b`, `a/b`},
		{`[^]`, `[\x00-\x{10FFFF}]`},
		{`(?<year>\d{4})`, `(?<year>\d{4})`},
		{`(?:ab)+`, `(?:ab)+`},
		// A surrogate pair is one code point, inside a class too.
		{`^\uD83D\uDE00$`, `^\x{1F600}$`},
		{`^[\uD83D\uDE00-\uD83D\uDE4F]$`, `^[\x{1F600}-\x{1F64F}]$`},
		{`^\u{1F600}$`, `^\x{1F600}$`},
		{`\x41\t\cJ\0`, `\x{41}\t\x{a}\x00`},
		{`[\b]`, `[\x08]`},
		{`\bword\B`, `\bword\B`},
		{`[\-\]a]`, `[\-\]a]`},
		{`\.\*\+\?\(\)\[\]\{\}\|\^\$\\`, `\.\*\+\?\(\)\[\]\{\}\|\^\$\\`},
		{`^\p{L}\P{Lu}\p{Script=Greek}\p{gc=Nd}$`, `^\p{L}\P{Lu}\p{Greek}\p{Nd}$`},
		// A [ inside a class is a literal, never an RE2 POSIX class.
		{`^[[:digit:]]+$`, `^[\[:digit:]]+$`},
	}
	for _, c := range cases {
		got, err := translatePattern(c.in)
		if err != nil || got != c.want {
			t.Errorf("translatePattern(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{
		`(?!a)`, `(?<=a)b`, `(a)\1`, `\k<x>`, `[\S]`, `\u12`, `[]`,
		// Lone surrogates match nothing RE2 can see.
		`\uD83D`, `\uDE00`, `\uD83Dx`, `\u{D83D}`, `\u{110000}`,
		// RE2 escapes and groups that are SyntaxErrors in u-mode ECMA-262.
		`^\a$`, `^a\z`, `^\Qa.b\E$`, `\pL`, `\A`, `\e`, `\q`, `\x4`, `\c1`, `[\B]`, `[\1]`,
		`(?i)abc`, `(?P<n>a)`, `(?s:.)`,
	} {
		if got, err := translatePattern(bad); err == nil {
			t.Errorf("translatePattern(%q) = %q, want an error", bad, got)
		}
	}
}

// The translated pattern matches what ECMA-262 (u flag) matches; the
// expectations were checked with node's RegExp.
func TestTranslatedPatternsMatchLikeECMA(t *testing.T) {
	cases := []struct {
		pattern, input string
		match          bool
	}{
		{`^\uD83D\uDE00$`, "\U0001F600", true},
		{`^[\uD83D\uDE00-\uD83D\uDE4F]$`, "\U0001F64F", true},
		{`^[\uD83D\uDE00-\uD83D\uDE4F]$`, "\U0001F650", false},
		{`^[[:digit:]]+$`, "123", false},
		{`^[[:digit:]]+$`, "d]", true},
		{`^a\-b\@$`, "a-b@", true},
		{`^[\b]$`, "\b", true},
	}
	for _, c := range cases {
		re, err := translatePattern(c.pattern)
		if err != nil {
			t.Fatalf("translatePattern(%q): %v", c.pattern, err)
		}
		if got := regexp.MustCompile(re).MatchString(c.input); got != c.match {
			t.Errorf("%q on %q = %v, want %v", c.pattern, c.input, got, c.match)
		}
	}
}
