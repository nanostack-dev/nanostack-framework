package generrors

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
			t.Fatalf("%v; run go test ./gen/errors -update", readErr)
		}
		if !bytes.Equal(want, f.Data) {
			t.Errorf("%s differs from generator output; run go test ./gen/errors -update and review the diff", f.Path)
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
			want: "no error declarations; add an interface type whose doc comment has a //nano:errors namespace=<name> line",
		},
		"bad namespace": {
			src:  declHeader + "//nano:errors namespace=Billing\ntype d interface {\n//nano:error code=a status=404 title=\"x\"\nA()\n}\n",
			want: `errors.go:8: //nano:errors: namespace="Billing" must be lower snake case`,
		},
		"missing directive": {
			src:  decl + "// Doc only.\nInvoiceGone()\n}\n",
			want: "errors.go:10: InvoiceGone: missing //nano:error line; add //nano:error code=invoice_gone status=",
		},
		"missing code": {
			src:  decl + "//nano:error status=400 title=\"x\"\nIPv6Invalid()\n}\n",
			want: "errors.go:10: IPv6Invalid: code is required; add code=ipv6_invalid",
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
			want: `unknown option "tittle"; valid options: code, status, title, detail`,
		},
		"duplicate code": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA()\n//nano:error status=404 title=\"y\" code=a\nB()\n}\n",
			want: `B: code "a" is already used by A; set a distinct code=`,
		},
		"reserved field": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(wrap string)\n}\n",
			want: `parameter "wrap" clashes with a generated name`,
		},
		"same field twice": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(url string, URL string)\n}\n",
			want: `A: parameters "url" and "URL" both become field URL; rename one`,
		},
		"unimported type": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA(id uuid.UUID)\n}\n",
			want: "errors.go:10:6: undefined: uuid",
		},
		"results": {
			src:  decl + "//nano:error code=a status=404 title=\"x\"\nA() error\n}\n",
			want: "declarations take parameters only; remove the results",
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
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := moduleTempDir(t, "bad-")
			writeDecl(t, dir, tc.src)
			_, err := Generate(Options{Dir: dir})
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
	if err == nil || !strings.Contains(err.Error(), "does not compile with the new errors.gen.go") ||
		!strings.Contains(err.Error(), "use.go:3:") {
		t.Fatalf("got %v, want the hand-written call reported", err)
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
