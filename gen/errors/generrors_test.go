package generrors

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
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

const fixture = "testdata/billing"

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
			t.Fatalf("%v; run go -C gen test ./errors -update", readErr)
		}
		if !bytes.Equal(want, f.Data) {
			t.Errorf(
				"%s differs from generator output; run go -C gen test ./errors -update and review the diff",
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
// own tests, which exercise the generated constructors, errors.Is/AsType,
// wrapping and the problem contract.
func TestGeneratedCodeCompilesAndBehaves(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	args := []string{"test", "-count=1", "./" + fixture}
	if raceEnabled {
		args = append(args, "-race")
	}
	out, err := exec.CommandContext(t.Context(), "go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go test %s: %v\n%s", fixture, err, out)
	}
}

// moduleTempDir returns an empty directory inside the module, removed after
// the test. Not t.TempDir: loading the package needs the module's imports.
func moduleTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("testdata", prefix) //nolint:usetesting // must sit inside the module
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func writeDecl(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "errors.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckReportsDriftAndWritesNothing(t *testing.T) {
	dir := moduleTempDir(t, "check-")
	src, err := os.ReadFile(filepath.Join(fixture, "errors.go"))
	if err != nil {
		t.Fatal(err)
	}
	writeDecl(t, dir, strings.Replace(string(src), "package billing", "package check", 1))

	assertStale(
		t,
		Run(Options{Dir: dir, Check: true}),
		filepath.Join(dir, "errors.gen.go"),
		filepath.Join(dir, "ERRORS.md"),
	)
	if _, err = os.Stat(filepath.Join(dir, "errors.gen.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("check mode wrote a file")
	}

	if err = Run(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err = Run(Options{Dir: dir, Check: true}); err != nil {
		t.Fatalf("check after generate: %v", err)
	}

	// A broken previous output must not block regeneration.
	out := filepath.Join(dir, "errors.gen.go")
	if err = os.WriteFile(out, []byte("package check\n\nfunc broken( {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStale(t, Run(Options{Dir: dir, Check: true}), out)
	if err = Run(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}

	doc := filepath.Join(dir, "ERRORS.md")
	if err = os.WriteFile(doc, []byte("hand edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStale(t, Run(Options{Dir: dir, Check: true}), doc)
	if code := Main([]string{"-dir", dir, "-check"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("Main -check exit code = %d, want 1", code)
	}
}

const declHeader = "package p\n\nimport \"time\"\n\nvar _ time.Time\n\n"

func TestDeclarationErrorsNameTheFix(t *testing.T) {
	const decl = declHeader + "//nano:errors namespace=b\ntype d interface {\n"
	cases := map[string]struct {
		src  string
		want string
	}{
		"no declaration": {
			src:  "package p\n",
			want: "no error declarations\n  fix: add an interface type whose doc comment has a //nano:errors namespace=<name> line",
		},
		"bad namespace": {
			src:  declHeader + "//nano:errors namespace=Billing\ntype d interface {\n//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
			want: `errors.go:8: //nano:errors: namespace="Billing" must be lower snake case`,
		},
		"missing directive": {
			src:  decl + "// Doc only.\nInvoiceGone()\n}\n",
			want: "errors.go:10: InvoiceGone: missing //nano:error line\n  fix: add //nano:error code=invoice_gone status=",
		},
		"missing code": {
			src:  decl + "//nano:error status=400 title=\"x\"\nIPv6Invalid()\n}\n",
			want: "errors.go:10: IPv6Invalid: code is required\n  fix: add code=ipv6_invalid",
		},
		"bad status": {
			src:  decl + "//nano:error code=a status=200 title=\"x\"\nA()\n}\n",
			want: `A: status="200" must be a known HTTP error status (400-599)`,
		},
		"missing title": {
			src:  decl + "//nano:error code=a status=404\nA()\n}\n",
			want: "A: title is required",
		},
		"unknown placeholder": {
			src:  decl + "//nano:error code=a status=404 title=\"x\" detail=\"no {idd}\"\nA(id string)\n}\n",
			want: "A: detail placeholder {idd} names no parameter",
		},
		"unknown option": {
			src:  decl + "//nano:error code=a status=404 title=\"x\" tittle=\"y\"\nA()\n}\n",
			want: `unknown option "tittle"` + "\n  fix: use one of the valid options: code, status, title, detail",
		},
		"duplicate code": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA()\n//nano:error status=404 title=\"y\" code=a\nB()\n}\n",
			want: `B: code "a" is already used by A` + "\n  fix: set a distinct code=",
		},
		"reserved field": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(wrap string)\n}\n",
			want: `parameter "wrap" clashes with a generated name`,
		},
		"same field twice": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(url string, URL string)\n}\n",
			want: `A: parameters "url" and "URL" both become field URL`,
		},
		"unimported type": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(id uuid.UUID)\n}\n",
			want: "errors.go:10: undefined: uuid",
		},
		"results": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA() error\n}\n",
			want: "A: declarations take parameters only\n  fix: remove the results",
		},
		"method named Kinds": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nKinds()\n}\n",
			want: "errors.go:10: Kinds: generated name Kinds clashes with the generated Kinds list",
		},
		"constructor clashes with a type": {
			src: decl + "//nano:error code=a status=404 title=\"x\"\nFoo()\n" +
				"//nano:error code=b status=404 title=\"y\"\nFooError()\n}\n",
			want: "errors.go:12: FooError: generated name FooError clashes with the generated code of Foo",
		},
		"clash with a hand-written name": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nFoo()\n}\n\nvar ErrFoo = 1\n",
			want: "errors.go:10: Foo: generated name ErrFoo clashes with ErrFoo declared at errors.go:13",
		},
		"clash with the Kinds list": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nFoo()\n}\n\nvar Kinds = 1\n",
			want: "errors.go:8: d: the generated Kinds list clashes with Kinds declared at errors.go:13",
		},
		"not an interface": {
			src:  declHeader + "//nano:errors namespace=b\ntype d struct{}\n",
			want: "errors.go:8: d: //nano:errors must annotate an interface type",
		},
		"two catalogues": {
			src: decl + "//nano:error code=a status=404 title=\"x\"\nA()\n}\n\n" +
				"//nano:errors namespace=c\ntype e interface {\n//nano:error code=b status=404 title=\"y\"\nB()\n}\n",
			want: "2 //nano:errors declarations",
		},
		"bad type_base": {
			src:  declHeader + "//nano:errors namespace=b type_base=ftp://x\ntype d interface {\n//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
			want: `type_base="ftp://x" must be an absolute http(s) URL`,
		},
		"embedded interface": {
			src:  decl + "error\n}\n",
			want: "d may only list methods, one per error",
		},
		"no methods": {
			src:  decl + "}\n",
			want: "d declares no errors",
		},
		"unexported method": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\na()\n}\n",
			want: "a: error names must be exported\n  fix: rename it to A",
		},
		"placeholder in title": {
			src:  decl + "//nano:error code=a status=404 title=\"no {id}\"\nA(id string)\n}\n",
			want: "A: title must not hold placeholders",
		},
		"bad code": {
			src:  decl + "//nano:error code=A-1 status=404 title=\"x\"\nA()\n}\n",
			want: `A: code="A-1" must be lower snake case`,
		},
		"variadic": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(ids ...string)\n}\n",
			want: "A: variadic parameters are not supported",
		},
		"unnamed parameter": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(string)\n}\n",
			want: "A: a parameter has no name",
		},
		"parameter shadows the type": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(AError string)\n}\n",
			want: `A: parameter "AError" shadows the generated type AError`,
		},
		"unmatched brace": {
			src:  decl + "//nano:error code=a status=404 title=\"x\" detail=\"no {id\"\nA(id string)\n}\n",
			want: "A: detail has an unmatched brace",
		},
		"not key=value": {
			src:  decl + "//nano:error code=a status=404 title\nA()\n}\n",
			want: `A: //nano:error: expected key=value at "title"`,
		},
		"option twice": {
			src:  decl + "//nano:error code=a code=b status=404 title=\"x\"\nA()\n}\n",
			want: "A: //nano:error: option code is set twice",
		},
		"unterminated quote": {
			src:  decl + "//nano:error code=a status=404 title=\"x\nA()\n}\n",
			want: "A: //nano:error: unterminated quoted value for title",
		},
		"bad quoted value": {
			src:  decl + "//nano:error code=a status=404 title=\"\\q\"\nA()\n}\n",
			want: "A: //nano:error: bad quoted value for title",
		},
		"catalogue option": {
			src:  declHeader + "//nano:errors namespace=b space=c\ntype d interface {\n//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
			want: `//nano:errors: unknown option "space"`,
		},
		"syntax error": {
			src:  declHeader + "func broken( {\n",
			want: "errors.go:7:",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := moduleTempDir(t, "bad-")
			writeDecl(t, dir, tc.src)
			_, err := Generate(Options{Dir: dir})
			gentest.Problems(t, err)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %v\nwant it to contain %q", err, tc.want)
			}
		})
	}
}

// TestValidDeclarationsCompile covers declarations whose output used to fail
// to compile. Generate type-checks its output, so success means it compiles;
// the test also builds the package with the go command to be sure.
func TestValidDeclarationsCompile(t *testing.T) {
	const iface = "//nano:errors namespace=b\ntype d interface {\n"
	cases := map[string]string{
		"doc mentions fmt.Sprintf": "package p\n\n" + iface +
			"// Built with fmt.Sprintf(\"%v\", x) in the old code.\n//\n" +
			"//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
		"import named fault": "package p\n\nimport fault \"net/netip\"\n\n" + iface +
			"//nano:error code=a status=400 title=\"x\" detail=\"bad {addr}\"\nBadAddr(addr fault.Addr)\n}\n",
		"package-level name fault": "package p\n\nvar fault = 1\n\nvar _ = fault\n\n" + iface +
			"//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
		"package name differs from path": "package p\n\nimport \"gopkg.in/yaml.v3\"\n\n" + iface +
			"//nano:error code=bad_node status=400 title=\"x\" detail=\"line {n}\"\nBadNode(n yaml.Node)\n}\n",
		"local type and time": "package p\n\nimport \"time\"\n\ntype ID string\n\n" + iface +
			"//nano:error code=a status=409 title=\"x\" detail=\"{id} at {at}\"\nA(id ID, at time.Time)\n}\n",
		"hand-written code calls the generated API": "package p\n\nfunc find() error { return A(\"x\") }\n\n" +
			"var _ = find\n\n" + iface + "//nano:error code=a status=404 title=\"x\"\nA(id string)\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := moduleTempDir(t, "ok-")
			writeDecl(t, dir, src)
			if err := Run(Options{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			if testing.Short() {
				return
			}
			out, err := exec.CommandContext(t.Context(), "go", "build", "./"+dir).CombinedOutput()
			if err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
		})
	}
}

// TestHandWrittenCodeMustMatch: verify reports hand-written code that no
// longer compiles against the new output, instead of writing it.
func TestHandWrittenCodeMustMatch(t *testing.T) {
	dir := moduleTempDir(t, "use-")
	writeDecl(t, dir, "package p\n\n"+
		"//nano:errors namespace=b\ntype d interface {\n//nano:error code=a status=404 title=\"x\"\nA(id string)\n}\n")
	use := "package p\n\nfunc find() error { return A() }\n\nvar _ = find\n"
	if err := os.WriteFile(filepath.Join(dir, "use.go"), []byte(use), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Generate(Options{Dir: dir})
	gentest.Problems(t, err)
	if err == nil || !strings.Contains(err.Error(), "use.go:3: not enough arguments in call to A") ||
		!strings.Contains(err.Error(), "fix: update the code that uses the generated errors.gen.go") {
		t.Fatalf("got %v, want the hand-written call reported", err)
	}
}

// TestOverlayReplacesTheDisk generates from an overlaid declaration, as a
// -check run of the pipeline does. The output file stays the generator's
// own, whatever the overlay holds.
func TestOverlayReplacesTheDisk(t *testing.T) {
	dir := moduleTempDir(t, "overlay-")
	const iface = "package p\n\n//nano:errors namespace=b\ntype d interface {\n//nano:error code=%s status=404 title=\"x\"\n%s()\n}\n"
	writeDecl(t, dir, fmt.Sprintf(iface, "on_disk", "OnDisk"))
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(Options{Dir: dir, Overlay: map[string][]byte{
		filepath.Join(abs, "errors.go"):     fmt.Appendf(nil, iface, "overlaid", "Overlaid"),
		filepath.Join(abs, "errors.gen.go"): []byte("package p\n\nfunc broken( {\n"),
	}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Contains(files[0].Data, []byte("func Overlaid()")) || bytes.Contains(files[0].Data, []byte("OnDisk")) {
		t.Fatalf("errors.gen.go was not generated from the overlay:\n%s", files[0].Data)
	}

	_, err = Generate(Options{Dir: dir, Overlay: map[string][]byte{"errors.go": nil}})
	if p := gentest.Problems(t, err); !strings.Contains(p[0].Msg, "overlay path errors.go is not absolute") {
		t.Fatalf("relative overlay path: got %v", err)
	}
}

func TestNaming(t *testing.T) {
	for in, want := range map[string]string{
		"InvoiceNotFound": "invoice_not_found",
		"HTTPTimeout":     "http_timeout",
		"UserID":          "user_id",
		"OAuth2Failed":    "oauth2_failed",
		"OAuthFailed":     "oauth_failed",
		"IPv6Invalid":     "ipv6_invalid",
		"BadIPv4":         "bad_ipv4",
		"OAuthorBanned":   "o_author_banned",
		"APIKeyMissing":   "api_key_missing",
		"A":               "a",
	} {
		if got := snakeName(in); got != want {
			t.Errorf("snakeName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"id":     "ID",
		"paidAt": "PaidAt",
		"apiKey": "APIKey",
		"userID": "UserID",
		"url":    "URL",
	} {
		if got := exportName(in); got != want {
			t.Errorf("exportName(%q) = %q, want %q", in, got, want)
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
