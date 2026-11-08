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
// with go generate ./gen/validate/internal/golden/...
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
	lr.once.Do(func() { lr.gp, lr.err = loadPackage(dir, DefaultOutput) })
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
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			spec := writeSpec(t, dir, c.edit)
			_, err := generateWith(Options{Spec: spec, Dir: dir}, loaded(t, dir))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
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

func TestParseSpecNormalisesVersions(t *testing.T) {
	v30 := `openapi: 3.0.3
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: number
      minimum: 1
      exclusiveMinimum: true
      maximum: 5
      nullable: true
    R:
      $ref: '#/components/schemas/A'
      maxLength: 3
`
	s, err := ParseSpec("v30", []byte(v30))
	if err != nil {
		t.Fatal(err)
	}
	a := s.lookupSchema(t, "A")
	if a.Minimum != nil || a.ExclusiveMinimum == nil || a.ExclusiveMinimum.Lit != "1" || a.Maximum.Lit != "5" ||
		!a.Nullable {
		t.Fatalf("3.0 schema = %+v", a)
	}
	v31 := strings.Replace(strings.Replace(v30, "3.0.3", "3.1.0", 1),
		"      minimum: 1\n      exclusiveMinimum: true\n", "      exclusiveMinimum: 1\n", 1)
	v31 = strings.Replace(v31, "type: number", "type: [number, \"null\"]", 1)
	v31 = strings.Replace(v31, "      nullable: true\n", "", 1)
	s, err = ParseSpec("v31", []byte(v31))
	if err != nil {
		t.Fatal(err)
	}
	a = s.lookupSchema(t, "A")
	if a.ExclusiveMinimum == nil || a.ExclusiveMinimum.Lit != "1" || !a.Nullable || len(a.Types) != 1 ||
		a.Types[0] != "number" {
		t.Fatalf("3.1 schema = %+v", a)
	}
}

func (s *Spec) lookupSchema(t *testing.T, name string) *Schema {
	t.Helper()
	ref := "#/components/schemas/" + name
	s.parseRef(ref, "test")
	sc := s.schemas[ref]
	if sc == nil {
		t.Fatalf("schema %s not parsed", name)
	}
	return sc
}
