package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

// A source whose generated models differ from the committed ones (CockroachDB and
// MariaDB) runs in a second test binary, compiled with `go test -c -overlay` so that
// every committed model file is replaced by the file generated from that source. The
// suite then compiles against, and exercises, that database's own generator output,
// as upstream did by regenerating .gentestdata before each of those runs. The parent
// process keeps the containers and passes their addresses to the child.

// childEnv carries the childSpec of a child run, as JSON.
const childEnv = "NANODB_INTERNAL_CHILD"

type childSpec struct {
	Source    Source            `json:"source"`
	Companion Source            `json:"companion,omitempty"`
	Databases map[Source]dbAddr `json:"databases"`
	SQLiteDir string            `json:"sqliteDir,omitempty"`
	TestRoot  string            `json:"testRoot"`
	WorkDir   string            `json:"workDir"`
	Models    string            `json:"models"`
}

type dbAddr struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// processStart approximates when the test binary started, to pass the remaining
// -test.timeout on to a child.
var processStart = time.Now()

// runChildMode runs s once for the source a parent harness prepared.
func runChildMode(m *testing.M, s Suite, raw string) int {
	var spec childSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		fmt.Fprintf(os.Stderr, "testenv: %s: %v\n", childEnv, err)
		return 2
	}
	h := &harness{
		suite:       s,
		sources:     []Source{spec.Source},
		sqliteDir:   spec.SQLiteDir,
		companionOf: make(map[Source]Source),
		dbs:         make(map[Source]*Database),
	}
	if spec.Companion != "" {
		h.companionOf[spec.Source] = spec.Companion
	}
	for src, a := range spec.Databases {
		h.dbs[src] = &Database{Source: src, Host: a.Host, Port: a.Port}
	}
	env := &Env{
		M:        m,
		Source:   spec.Source,
		DB:       h.dbs[spec.Source],
		TestRoot: spec.TestRoot,
		WorkDir:  spec.WorkDir,
		Models:   spec.Models,
	}
	h.configure(env)
	return h.runIn(env)
}

// runInChild builds the overlay test binary for env and runs it. It returns the
// child's exit code, or 1 when the suite does not compile against env's models.
func (h *harness) runInChild(env *Env, report string) int {
	overlay, err := h.writeOverlay(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	bin := filepath.Join(h.tmp, "bin", h.suite.Name+"-"+string(env.Source)+".test")

	progressf("testenv: %s: the %s models differ from the committed %s models; compiling the suite against them:\n%s",
		h.suite.Name, env.Source, committedFor(env.Source), report)
	start := time.Now()
	args := append([]string{"test", "-c", "-o", bin, "-overlay", overlay}, buildFlags(h.suite.DropTags[env.Source])...)
	args = append(args, ".")
	build := exec.CommandContext(context.Background(), "go", args...)
	build.Dir = h.pkgDir
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		progressf(
			"testenv: %s: FAIL: the suite does not compile against the models generated from %s:\n%s\n",
			h.suite.Name,
			env.Source,
			out,
		)
		return 1
	}
	h.timings = append(h.timings, timing{string(env.Source) + " overlay build", time.Since(start)})

	spec := childSpec{
		Source: env.Source, Databases: make(map[Source]dbAddr), SQLiteDir: h.sqliteDir,
		TestRoot: env.TestRoot, WorkDir: env.WorkDir, Models: ModelsGenerated,
	}
	if c, ok := h.companion(env.Source); ok {
		spec.Companion = c
	}
	for src, d := range h.dbs {
		spec.Databases[src] = dbAddr{Host: d.Host, Port: d.Port}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	childArgs, err := childTestArgs(os.Args[1:], time.Since(processStart))
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}

	cmd := exec.CommandContext(context.Background(), bin, childArgs...)
	cmd.Dir = env.WorkDir
	cmd.Env = append(os.Environ(), childEnv+"="+string(raw))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0:
		return exitErr.ExitCode()
	default:
		fmt.Fprintf(os.Stderr, "testenv: run %s: %v\n", bin, err)
		return 1
	}
}

// writeOverlay maps every committed model file of env's source to the generated one:
// a file only in the committed tree is deleted, a file only in the generated tree added.
func (h *harness) writeOverlay(env *Env) (string, error) {
	replace := map[string]string{}
	tests := repo.GetTestsDirPath()
	for _, dir := range ModelDirs(env.Source) {
		committedDir := filepath.Join(tests, GenTestDataDir, dir)
		generatedDir := filepath.Join(env.TestRoot, GenTestDataDir, dir)
		committed, err := readTree(committedDir)
		if err != nil {
			return "", err
		}
		generated, err := readTree(generatedDir)
		if err != nil {
			return "", err
		}
		for name := range committed {
			replace[filepath.Join(committedDir, filepath.FromSlash(name))] = ""
		}
		for name := range generated {
			replace[filepath.Join(committedDir, filepath.FromSlash(name))] = filepath.Join(
				generatedDir,
				filepath.FromSlash(name),
			)
		}
	}
	data, err := json.MarshalIndent(struct{ Replace map[string]string }{replace}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(h.tmp, "overlay-"+string(env.Source)+".json")
	return path, os.WriteFile(path, data, 0o600)
}

// buildFlags returns the build flags of the running test binary that change what a
// child compiles: -race and -tags, without the tags in drop.
func buildFlags(drop []string) []string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	var flags []string
	for _, s := range info.Settings {
		switch {
		case s.Key == "-race" && s.Value == "true":
			flags = append(flags, "-race")
		case s.Key == "-tags" && s.Value != "":
			var tags []string
			for tag := range strings.SplitSeq(s.Value, ",") {
				if !slices.Contains(drop, tag) {
					tags = append(tags, tag)
				}
			}
			if len(tags) > 0 {
				flags = append(flags, "-tags", strings.Join(tags, ","))
			}
		}
	}
	return flags
}

// childTestArgs returns the parent's -test.* arguments for the child: the same run,
// verbosity, count, shuffle and short flags, the remaining part of -test.timeout, and
// none of the output files the parent writes itself (test log, profiles, coverage).
func childTestArgs(args []string, elapsed time.Duration) ([]string, error) {
	parentOnly := map[string]bool{
		"test.testlogfile": true, "test.coverprofile": true, "test.cpuprofile": true,
		"test.memprofile": true, "test.blockprofile": true, "test.mutexprofile": true,
		"test.trace": true, "test.outputdir": true, "test.gocoverdir": true, "test.timeout": true,
	}
	var out []string
	for i := 0; i < len(args); i++ {
		name, _, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !parentOnly[name] {
			out = append(out, args[i])
			continue
		}
		if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++ // the value of a two-argument flag
		}
	}
	if f := flag.Lookup("test.timeout"); f != nil {
		if timeout, err := time.ParseDuration(f.Value.String()); err == nil && timeout > 0 {
			remaining := timeout - elapsed
			if remaining <= 0 {
				return nil, fmt.Errorf("no time left of -test.timeout=%s for the child run", timeout)
			}
			out = append(out, "-test.timeout="+remaining.String())
		}
	}
	return out, nil
}
