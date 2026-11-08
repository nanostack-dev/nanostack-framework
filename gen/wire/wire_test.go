package wire

import (
	"bufio"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

var update = flag.Bool("update", false, "rewrite golden files")

// okCases generate code; the golden file is the generated file on disk, so
// the golden test is also the --check of the committed testdata.
var okCases = []struct {
	name, out string
}{
	{"signatures", ""},
	{"samepkg", "."},
	{"example", ""},
	{"adapter", ""},
	{"collect", ""},
}

// errCases fail generation; errors.golden holds the expected diagnostics.
var errCases = []string{
	"missing",
	"duplicate",
	"cycle",
	"unused",
	"badsig",
	"valuerecv",
	"access",
	"collecterr",
	"collectdup",
	"collectuse",
}

// generate runs Generate and returns its single file.
func generate(t *testing.T, opts Options) gen.File {
	t.Helper()
	files, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Generate returned %d files, want 1", len(files))
	}
	return files[0]
}

func caseOptions(t *testing.T, name, out string) Options {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Options{Dir: dir, Out: out, Base: dir}
}

func TestGoldenOutput(t *testing.T) {
	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			res := generate(t, caseOptions(t, tc.name, tc.out))
			if *update {
				if _, err := gen.Sync("wire", []gen.File{res}, false); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(res.Path)
			if err != nil {
				t.Fatalf("%v (run go test ./gen/wire -update)", err)
			}
			if string(want) != string(res.Data) {
				t.Fatalf("%s is stale; run go test ./gen/wire -update and review the diff\n--- generated:\n%s",
					res.Path, res.Data)
			}
		})
	}
}

func TestGoldenErrors(t *testing.T) {
	for _, name := range errCases {
		t.Run(name, func(t *testing.T) {
			_, err := Generate(caseOptions(t, name, ""))
			gerr, ok := errors.AsType[*Error](err)
			if !ok {
				t.Fatalf("Generate = %v, want *Error", err)
			}
			got := gerr.Error() + "\n"
			golden := filepath.Join("testdata", name, "errors.golden")
			if *update {
				if werr := os.WriteFile(golden, []byte(got), 0o600); werr != nil {
					t.Fatal(werr)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("diagnostics differ\n--- got:\n%s--- want:\n%s", got, want)
			}
		})
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	opts := caseOptions(t, "signatures", "")
	first, second := generate(t, opts), generate(t, opts)
	if string(first.Data) != string(second.Data) {
		t.Fatal("two runs produced different bytes")
	}
}

// TestGeneratedCodeCompiles type-checks every ok case as a normal build
// would, without the nanogen tag, so the manifest file is excluded and
// wire.gen.go is included.
func TestGeneratedCodeCompiles(t *testing.T) {
	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &packages.Config{
				Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
				Dir:  filepath.Join("testdata", tc.name),
			}
			pkgs, err := packages.Load(cfg, "./...")
			if err != nil {
				t.Fatal(err)
			}
			if n := packages.PrintErrors(pkgs); n > 0 {
				t.Fatalf("%d compile errors", n)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	check := func(out string) Options {
		opts := caseOptions(t, "signatures", out)
		opts.Check = true
		return opts
	}
	if err := Run(check("")); err != nil {
		t.Fatalf("Check on fresh output = %v", err)
	}
	if err := Run(check("internal/gen/absent")); !errors.Is(err, gen.ErrStale) {
		t.Fatalf("Check with no output = %v, want gen.ErrStale", err)
	}

	opts := check("internal/gen/stale")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(opts.Dir, opts.Out)) })
	res := generate(t, opts)
	res.Data = append(res.Data, []byte("// hand edit\n")...)
	if _, err := gen.Sync("wire", []gen.File{res}, false); err != nil {
		t.Fatal(err)
	}
	if err := Run(opts); !errors.Is(err, gen.ErrStale) {
		t.Fatalf("Check with an edited file = %v, want gen.ErrStale", err)
	}
	opts.Check = false
	if err := Run(opts); err != nil {
		t.Fatalf("Run = %v", err)
	}
	opts.Check = true
	if err := Run(opts); err != nil {
		t.Fatalf("Check after Run rewrote the file = %v", err)
	}
}

func TestNoManifest(t *testing.T) {
	opts := caseOptions(t, "signatures/lib", "")
	_, err := Generate(opts)
	if err == nil || !strings.Contains(err.Error(), "no manifest") {
		t.Fatalf("Generate = %v, want no-manifest error", err)
	}
}

func TestNaming(t *testing.T) {
	for in, want := range map[string]string{
		"PGStore": "pgStore", "DB": "db", "Service": "service", "HTTPServer": "httpServer",
		"URL": "url", "x": "x", "ID": "id", "IDs": "ids", "Users": "users",
	} {
		if got := lowerCamel(in); got != want {
			t.Errorf("lowerCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExampleService builds the example's binary, runs it, probes it, sends
// SIGTERM and expects a clean exit; then it runs the example's own tests,
// which exercise Overrides. testdata is outside ./..., so this is the only
// test that builds the example.
func TestExampleService(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a binary")
	}
	bin := filepath.Join(t.TempDir(), "example")
	goCmd(t, "build", "-o", bin, "./testdata/example/cmd/example")

	cmd := exec.CommandContext(t.Context(), bin)
	cmd.Env = append(os.Environ(), "EXAMPLE_ADDR=127.0.0.1:0", "EXAMPLE_ADMIN_ADDR=127.0.0.1:0")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var logs []string
	addrRe := regexp.MustCompile(`msg=listening addr=(\S+) admin_addr=(\S+)`)
	var addr, adminAddr string
	timeout := time.After(30 * time.Second)
	for addr == "" {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("example exited before listening:\n%s", strings.Join(logs, "\n"))
			}
			logs = append(logs, l)
			if m := addrRe.FindStringSubmatch(l); m != nil {
				addr, adminAddr = m[1], m[2]
			}
		case <-timeout:
			t.Fatalf("example did not listen:\n%s", strings.Join(logs, "\n"))
		}
	}

	if body := get(t, "http://"+addr+"/hello/ada", http.StatusOK); body != "Hello, ada\n" {
		t.Fatalf("/hello/ada = %q", body)
	}
	get(t, "http://"+adminAddr+"/readyz", http.StatusOK)
	get(t, "http://"+addr+"/readyz", http.StatusNotFound) // the probes stay off the API's port
	get(t, "http://"+addr+"/hello/nobody", http.StatusNotFound)

	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for l := range lines {
		logs = append(logs, l)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("exit after SIGTERM: %v\n%s", err, strings.Join(logs, "\n"))
	}
	all := strings.Join(logs, "\n")
	stopping, closed := strings.Index(all, "nano: stopping"), strings.Index(all, "store closed")
	if stopping < 0 || closed < stopping || !strings.Contains(all, "msg=\"nano: stopped\"") {
		t.Fatalf("shutdown did not stop the store after stopping began:\n%s", all)
	}

	goCmd(t, "test", "-count=1", "./testdata/example/...")
}

// TestGeneratedBehaviour runs the tests inside the adapter and collect
// testdata modules: an adapter provider must not run a component twice, and
// collections and BuildComponents must mirror echopoint's fx groups and
// fx.Populate.
func TestGeneratedBehaviour(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on testdata packages")
	}
	goCmd(t, "test", "-count=1", "./testdata/adapter/...", "./testdata/collect/...")
}

func goCmd(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func get(t *testing.T, url string, wantCode int) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantCode {
		t.Fatalf("GET %s = %d %q, want %d", url, resp.StatusCode, body, wantCode)
	}
	return string(body)
}
