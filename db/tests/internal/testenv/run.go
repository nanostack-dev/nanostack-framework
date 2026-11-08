package testenv

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

// Suite describes one test package and how it runs against one source.
type Suite struct {
	// Name labels the package in messages, e.g. "postgres".
	Name string
	// Sources lists the sources this package covers, in run order.
	Sources []Source
	// Run runs the package's tests once against env, typically by calling env.M.Run
	// one or more times, and returns the exit code.
	Run func(env *Env) int
	// DropTags lists, per source, build tags left out when the suite is compiled against
	// that source's own models (see child.go). Upstream's postgres tag gates the
	// PostgreSQL-only range tests, which upstream never compiled for CockroachDB.
	DropTags map[Source][]string
}

// Env is the environment of one suite run.
type Env struct {
	M      *testing.M
	Source Source
	// DB is the server under test; nil for SQLite.
	DB *Database
	// TestRoot is a stand-in for db/tests whose .gentestdata holds the models freshly
	// generated from Source; the suites read generated files through it at run time.
	TestRoot string
	// WorkDir is TestRoot/<suite name>, the working directory of the run. The suites
	// write generator output relative to it (./.gentestdata2, ./.tempTestDir) and read
	// expected results from ../testdata, so two runs never share a directory.
	WorkDir string
	// Models says which models the running binary was compiled against.
	Models string
}

// Models values: the binary was compiled against the committed models, or against the
// models generated from Env.Source (an overlay child, or a source whose generator output
// equals the committed models).
const (
	ModelsCommitted = "committed"
	ModelsGenerated = "generated"
)

// Run starts what the selected sources of s need, runs s once per source and stops
// everything. Call it from TestMain: os.Exit(testenv.Run(m, suite)).
func Run(m *testing.M, s Suite) int {
	if !flag.Parsed() {
		flag.Parse()
	}
	if raw := os.Getenv(childEnv); raw != "" {
		return runChildMode(m, s, raw)
	}

	selected, err := Selected()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 2
	}
	sources := intersect(s.Sources, selected)

	if testing.Short() {
		var skipped []Source
		sources, skipped = partition(sources, func(src Source) bool { return !src.needsContainer() })
		if len(skipped) > 0 {
			progressf("testenv: %s: skipped %s: -short runs no container suites\n", s.Name, join(skipped))
		}
	}
	if len(sources) == 0 {
		if !testing.Short() {
			progressf(
				"testenv: %s: skipped: %s=%q selects none of %s\n",
				s.Name,
				DialectsEnv,
				os.Getenv(DialectsEnv),
				join(s.Sources),
			)
		}
		return runNothing(m)
	}

	h := &harness{suite: s, sources: sources}
	defer h.close()

	if err = h.setUp(); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}

	code := 0
	for _, src := range sources {
		env := h.envs[src]
		env.M = m

		progressf("\ntestenv: %s: running against %s %s\n", s.Name, src, imageLabel(src))
		start := time.Now()
		var c int
		if report := h.reports[src]; report != "" && !IsCommitted(src) {
			c = h.runInChild(env, report)
		} else {
			env.Models = ModelsCommitted
			if report == "" {
				env.Models = ModelsGenerated // identical trees: the committed models are this source's
			}
			h.configure(env)
			c = h.runIn(env)
		}
		if c != 0 {
			progressf("testenv: %s: FAIL against %s\n", s.Name, src)
			code = c
		}
		h.timings = append(h.timings, timing{string(src) + " tests", time.Since(start)})
	}

	h.printTimings()
	return code
}

// runIn runs the suite with env.WorkDir as the working directory.
func (h *harness) runIn(env *Env) int {
	prev, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	if err = os.Chdir(env.WorkDir); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	defer func() { _ = os.Chdir(prev) }()
	return h.runWithKnownFailures(env)
}

// runNothing lets the package report "[no tests to run]" instead of silently passing.
func runNothing(m *testing.M) int {
	_ = flag.Set("test.run", "^$")
	_ = flag.Set("test.bench", "")
	return m.Run()
}

type timing struct {
	what string
	took time.Duration
}

type harness struct {
	suite   Suite
	sources []Source

	pkgDir      string // the suite's package directory, the working directory go test gave
	tmp         string
	dataDir     string
	sqliteDir   string
	dbs         map[Source]*Database
	envs        map[Source]*Env
	reports     map[Source]string // how each source's generated models differ from the committed ones
	companionOf map[Source]Source // set in a child run: the companion the parent chose
	timings     []timing
}

func (h *harness) setUp() error {
	start := time.Now()
	var err error
	if h.pkgDir, err = os.Getwd(); err != nil {
		return err
	}
	if slices.ContainsFunc(h.sources, Source.needsContainer) {
		if err = requireRyuk(); err != nil {
			return err
		}
	}
	if h.dataDir, err = TestData(); err != nil {
		return err
	}
	if h.tmp, err = newRunDir(h.suite.Name); err != nil {
		return err
	}
	if err = InstallJetCLI(filepath.Join(h.tmp, "bin")); err != nil {
		return err
	}
	h.timings = append(h.timings, timing{"test data + jet CLI", time.Since(start)})

	start = time.Now()
	if err = h.startDatabases(); err != nil {
		return err
	}
	h.timings = append(h.timings, timing{"containers (parallel)", time.Since(start)})

	h.envs = make(map[Source]*Env)
	h.reports = make(map[Source]string)
	for _, src := range h.sources {
		start = time.Now()
		env, envErr := h.newEnv(src)
		if envErr != nil {
			return envErr
		}
		h.envs[src] = env
		if h.reports[src], err = DiffModels(src, repo.GetTestsDirPath(), env.TestRoot, 0); err != nil {
			return err
		}
		h.timings = append(h.timings, timing{string(src) + " models", time.Since(start)})
	}
	return nil
}

// companions returns the servers a source's suite also reaches. The CockroachDB run of
// the postgres suite reads dbconfig.Pg* in its generator tests, and the MariaDB run of
// the mysql suite reads dbconfig.MySQL*, exactly as upstream CI ran them with every
// service up.
func (h *harness) companion(src Source) (Source, bool) {
	if c, ok := h.companionOf[src]; ok {
		return c, true
	}
	switch src {
	case Cockroach:
		for _, pg := range slices.Backward(h.sources) {
			if pg.IsPostgres() {
				return pg, true
			}
		}
		return Postgres18, true
	case MariaDB:
		return MySQL, true
	default:
		return "", false
	}
}

func (h *harness) startDatabases() error {
	var needed []Source
	for _, src := range h.sources {
		if src.needsContainer() && !slices.Contains(needed, src) {
			needed = append(needed, src)
		}
		if c, ok := h.companion(src); ok && !slices.Contains(needed, c) {
			needed = append(needed, c)
		}
	}

	h.dbs = make(map[Source]*Database)
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []error
	)
	for _, src := range needed {
		wg.Go(func() {
			start := time.Now()
			d, err := Start(context.Background(), src, h.dataDir)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			h.dbs[src] = d
			h.timings = append(h.timings, timing{fmt.Sprintf("  %s start+load", src), time.Since(start)})
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (h *harness) newEnv(src Source) (*Env, error) {
	env := &Env{Source: src, DB: h.dbs[src]}

	if src == SQLite {
		if err := h.copySQLite(); err != nil {
			return nil, err
		}
	}

	env.TestRoot = filepath.Join(h.tmp, "root-"+string(src))
	env.WorkDir = filepath.Join(env.TestRoot, h.suite.Name)
	if err := os.MkdirAll(env.WorkDir, 0o750); err != nil {
		return nil, err
	}
	if err := os.Symlink(repo.GetTestsFilePath("testdata"), filepath.Join(env.TestRoot, "testdata")); err != nil {
		return nil, err
	}
	if err := GenerateModels(src, env.DB, h.sqliteDir, env.TestRoot); err != nil {
		return nil, err
	}
	return env, nil
}

// copySQLite copies the SQLite databases into the run's temp dir: some suites write
// to them, and the test data cache is shared by every run.
func (h *harness) copySQLite() error {
	h.sqliteDir = filepath.Join(h.tmp, "sqlite")
	return CopySQLite(h.dataDir, h.sqliteDir)
}

// CopySQLite copies the SQLite test databases of dataDir into dir. Nothing may open the
// files of the shared cache directly: SQLite writes to a database it opens read-write.
func CopySQLite(dataDir, dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, file := range sqliteFiles {
		if err := copyFile(filepath.Join(dataDir, "init", "sqlite", file), filepath.Join(dir, file)); err != nil {
			return err
		}
	}
	return nil
}

// configure points dbconfig at the servers of env's run.
func (h *harness) configure(env *Env) {
	use := func(src Source) {
		d := h.dbs[src]
		switch {
		case src.IsPostgres():
			dbconfig.UsePostgres(d.Host, d.Port)
		case src == Cockroach:
			dbconfig.UseCockroach(d.Host, d.Port)
		case src == MySQL:
			dbconfig.UseMySQL(d.Host, d.Port)
		case src == MariaDB:
			dbconfig.UseMariaDB(d.Host, d.Port)
		}
	}
	if c, ok := h.companion(env.Source); ok {
		use(c)
	}
	switch env.Source {
	case SQLite:
		dbconfig.UseSQLite(
			filepath.Join(h.sqliteDir, "sakila.db"),
			filepath.Join(h.sqliteDir, "chinook.db"),
			filepath.Join(h.sqliteDir, "test_sample.db"),
		)
	default:
		use(env.Source)
	}
}

func (h *harness) close() {
	var wg sync.WaitGroup
	for src, d := range h.dbs {
		wg.Go(func() {
			if err := d.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "testenv: stop %s: %v\n", src, err)
			}
		})
	}
	wg.Wait()
	if h.tmp != "" {
		_ = os.RemoveAll(h.tmp)
	}
}

func (h *harness) printTimings() {
	progressf("\ntestenv: %s wall-clock:\n", h.suite.Name)
	for _, t := range h.timings {
		progressf("testenv:   %-28s %6.1fs\n", t.what, t.took.Seconds())
	}
}

func imageLabel(src Source) string {
	if img := images[src]; img != "" {
		return "(" + img + ")"
	}
	return "(file)"
}

func intersect(want, selected []Source) []Source {
	var out []Source
	for _, s := range want {
		if slices.Contains(selected, s) {
			out = append(out, s)
		}
	}
	return out
}

func partition(in []Source, keep func(Source) bool) (kept, dropped []Source) {
	for _, s := range in {
		if keep(s) {
			kept = append(kept, s)
		} else {
			dropped = append(dropped, s)
		}
	}
	return kept, dropped
}

// progressf prints harness progress on standard output, where go test shows it next to
// the test results.
func progressf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, format, args...)
}

func join(sources []Source) string {
	parts := make([]string, len(sources))
	for i, s := range sources {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- test data
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst) // #nosec G304 -- temp dir
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
