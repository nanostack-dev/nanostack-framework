// Command upstreamsync compares Nanostack DB with a go-jet release and, on request, merges it.
//
// It reads the upstream base commit from db/UPSTREAM.md, exports the base and the requested
// upstream ref with `git archive`, and applies the same rewrite as the original import
// (module path and per-file header). It then prints:
//
//  1. upstream changes: what go-jet changed between the base and the ref, after rewriting;
//  2. Nanostack changes: where this module diverges from the rewritten base.
//
// With -apply it also merges every upstream change into the module with `git merge-file`
// and lists the conflicts. Run it from the db directory:
//
//	go run ./tools/upstreamsync -ref v2.17.0
//	go run ./tools/upstreamsync -ref v2.17.0 -apply
//
// See "Merging an upstream release" in db/UPSTREAM.md for the full procedure.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultUpstreamURL = "https://github.com/go-jet/jet.git"

// testDataPath is the upstream submodule that pins github.com/go-jet/jet-test-data.
const testDataPath = "tests/testdata"

// excluded reports whether a path is never compared or merged: generated test models,
// which the fork regenerates itself.
func excluded(p string) bool {
	return p == "tests/.gentestdata" || strings.HasPrefix(p, "tests/.gentestdata/")
}

type options struct {
	dbDir    string // the db module root
	ref      string // upstream tag, branch or SHA to compare with
	base     string // upstream base; empty means the one recorded in UPSTREAM.md
	repo     string // existing go-jet clone to use without fetching
	url      string // upstream URL for the cache
	cacheDir string // bare mirror of url
	apply    bool   // merge upstream changes into dbDir
	stat     bool   // print file lists only, no patches
}

func main() {
	var o options
	flag.StringVar(&o.dbDir, "db", ".", "path to the db module root")
	flag.StringVar(&o.ref, "ref", "", "upstream tag, branch or commit to compare with (required)")
	flag.StringVar(&o.base, "base", "", "upstream base commit (default: the Base commit row of UPSTREAM.md)")
	flag.StringVar(&o.repo, "repo", "", "use this existing go-jet clone and do not fetch")
	flag.StringVar(&o.url, "upstream", defaultUpstreamURL, "upstream repository URL")
	flag.StringVar(&o.cacheDir, "cache", "", "directory of the bare upstream mirror (default: user cache dir)")
	flag.BoolVar(&o.apply, "apply", false, "merge the upstream changes into the module (3-way, per file)")
	flag.BoolVar(&o.stat, "stat", false, "print the changed files only, without patches")
	flag.Parse()
	if o.ref == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if o.cacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "upstreamsync:", err)
			os.Exit(2)
		}
		o.cacheDir = filepath.Join(dir, "nanostack", "db-upstream", "go-jet.git")
	}
	conflicts, err := run(context.Background(), o, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "upstreamsync:", err)
		os.Exit(2)
	}
	if conflicts > 0 {
		os.Exit(1)
	}
}

var baseCommitRow = regexp.MustCompile("(?m)^\\|\\s*Base commit\\s*\\|\\s*`([0-9a-f]{40})`")

// recordedBase returns the base commit from the "Base commit" row of UPSTREAM.md.
func recordedBase(dbDir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dbDir, "UPSTREAM.md"))
	if err != nil {
		return "", err
	}
	m := baseCommitRow.FindSubmatch(content)
	if m == nil {
		return "", errors.New("UPSTREAM.md has no `| Base commit | `<40-char sha>` |` row")
	}
	return string(m[1]), nil
}

func checkModule(dbDir string) error {
	content, err := os.ReadFile(filepath.Join(dbDir, "go.mod"))
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`(?m)^module ` + regexp.QuoteMeta(ForkModule) + `\s*$`).Match(content) {
		return fmt.Errorf("%s is not the root of module %s (use -db)", dbDir, ForkModule)
	}
	return nil
}

// run prints the comparison and, with o.apply, merges. It returns the number of conflicts.
func run(ctx context.Context, o options, w io.Writer) (int, error) {
	if err := checkModule(o.dbDir); err != nil {
		return 0, err
	}
	work, err := os.MkdirTemp("", "upstreamsync-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)

	s, err := export(ctx, o, work)
	if err != nil {
		return 0, err
	}
	sc, err := newScratch(ctx, work)
	if err != nil {
		return 0, err
	}
	trees := map[string]string{}
	for name, dir := range map[string]string{"base": s.dirs.base, "ref": s.dirs.ref, "ours": s.dirs.ours} {
		if trees[name], err = sc.tree(ctx, dir); err != nil {
			return 0, err
		}
	}

	pinFrom := submoduleCommit(ctx, s.repo, s.base, testDataPath)
	pinTo := submoduleCommit(ctx, s.repo, s.ref, testDataPath)
	ignored, err := printPreamble(ctx, w, o, s, pinFrom, pinTo)
	if err != nil {
		return 0, err
	}
	if err = printSections(ctx, w, o, s, sc, trees); err != nil {
		return 0, err
	}
	if !o.apply {
		return len(ignored), nil
	}
	conflicts, err := applyRef(ctx, w, o, s, sc, trees["base"], pinTo != pinFrom && pinTo != "", pinTo)
	return conflicts + len(ignored), err
}

// exported holds the rewritten upstream sides of one run and the paths each contains.
type exported struct {
	repo, base, ref     string // the upstream repository and the resolved base and ref commits
	work                string // the scratch directory that holds dirs
	dirs                sideDirs
	basePaths, refPaths map[string]bool
}

// sideDirs are the rewritten trees under the work directory. refApply is the ref with its
// own header, which -apply merges; the others carry the header our files have.
type sideDirs struct{ base, ref, refApply, ours string }

// export resolves the base and the ref, writes both rewritten sides and our side into
// work, and returns where they are.
func export(ctx context.Context, o options, work string) (exported, error) {
	recorded, recordedErr := recordedBase(o.dbDir)
	baseRef := o.base
	if baseRef == "" {
		if recordedErr != nil {
			return exported{}, recordedErr
		}
		baseRef = recorded
	}
	repo, err := upstreamRepo(ctx, o.repo, o.url, o.cacheDir, baseRef, o.ref)
	if err != nil {
		return exported{}, err
	}
	s := exported{repo: repo, work: work, dirs: sideDirs{
		base: filepath.Join(work, "base"), ref: filepath.Join(work, "ref"),
		refApply: filepath.Join(work, "ref-apply"), ours: filepath.Join(work, "ours"),
	}}
	if s.base, err = resolve(ctx, repo, baseRef); err != nil {
		return exported{}, err
	}
	if s.ref, err = resolve(ctx, repo, o.ref); err != nil {
		return exported{}, err
	}

	// Both upstream sides get the header our files carry (the recorded base, which differs
	// from base only with -base), so a header line never shows as a change. For -apply, the
	// ref is rendered a second time with its own header, which the merge carries into every
	// upstream file.
	headerSHA := shortSHA(s.base)
	if recordedErr == nil {
		headerSHA = shortSHA(recorded)
	}
	if s.basePaths, err = extract(ctx, repo, s.base, target{s.dirs.base, headerSHA}); err != nil {
		return exported{}, err
	}
	refTargets := []target{{s.dirs.ref, headerSHA}}
	if o.apply {
		refTargets = append(refTargets, target{s.dirs.refApply, shortSHA(s.ref)})
	}
	if s.refPaths, err = extract(ctx, repo, s.ref, refTargets...); err != nil {
		return exported{}, err
	}
	return s, copyOurs(o.dbDir, s.basePaths, s.dirs.ours)
}

// copyOurs copies our side into dir: only paths that exist in the base, read from where
// the fork keeps them (forkPath) and stored under the upstream path. Everything else is
// Nanostack-only.
func copyOurs(dbDir string, basePaths map[string]bool, dir string) error {
	for p := range basePaths {
		name := filepath.Join(dbDir, filepath.FromSlash(forkPath(p)))
		content, err := os.ReadFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := os.Stat(name)
		if err != nil {
			return err
		}
		if err = writeFile(filepath.Join(dir, filepath.FromSlash(p)), content, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// printPreamble prints the compared commits, the test data pins and the upstream files
// git ignores here, and returns those files.
func printPreamble(ctx context.Context, w io.Writer, o options, s exported, pinFrom, pinTo string) ([]string, error) {
	fmt.Fprintf(w, "upstream base  %s\nupstream ref   %s (%s)\n", s.base, s.ref, o.ref)
	if pinFrom != pinTo {
		fmt.Fprintf(w, "test data pin  %s -> %s  (TestDataCommit in %s and the Test data row of UPSTREAM.md; "+
			"-apply rewrites the constant)\n", pinFrom, pinTo, testDataCommitFile)
	} else {
		fmt.Fprintf(w, "test data pin  %s (unchanged)\n", pinFrom)
	}
	harness, harnessErr := harnessTestDataCommit(o.dbDir)
	if harnessErr == nil && pinFrom != "" && harness != pinFrom && harness != pinTo {
		fmt.Fprintf(w, "WARNING        TestDataCommit in %s is %s, upstream pins %s\n",
			testDataCommitFile, harness, pinTo)
	}
	ignored, err := ignoredUpstreamFiles(ctx, o.dbDir, s.refPaths)
	if err != nil {
		return nil, err
	}
	for _, p := range ignored {
		fmt.Fprintf(w, "IGNORED        %s: tracked upstream, but git ignores it here, so a commit drops it "+
			"(add a .gitignore negation)\n", p)
	}
	return ignored, nil
}

// printSections prints section 1 (upstream changes) and section 2 (Nanostack changes).
func printSections(
	ctx context.Context,
	w io.Writer,
	o options,
	s exported,
	sc *scratch,
	trees map[string]string,
) error {
	fmt.Fprintf(w, "\n== 1. Upstream changes (base -> ref, rewritten) ==\n")
	if err := printDiff(ctx, w, sc, trees["base"], trees["ref"], o.stat); err != nil {
		return err
	}
	var collisions []string
	for p := range s.refPaths {
		if s.basePaths[p] {
			continue
		}
		if _, err := os.Stat(filepath.Join(o.dbDir, filepath.FromSlash(forkPath(p)))); err == nil {
			collisions = append(collisions, p)
		}
	}
	if len(collisions) > 0 {
		fmt.Fprintf(w, "\nAdded upstream but already present in Nanostack DB: %s\n",
			strings.Join(sortStrings(collisions), ", "))
	}

	fmt.Fprintf(w, "\n== 2. Nanostack changes (rewritten base -> db, files from upstream only) ==\n")
	return printDiff(ctx, w, sc, trees["base"], trees["ours"], o.stat)
}

// applyRef merges the ref into the module, rewrites TestDataCommit when the pin moved
// (pinMoved, to pinTo), prints section 3 and returns the number of conflicts.
func applyRef(
	ctx context.Context, w io.Writer, o options, s exported, sc *scratch, baseTree string, pinMoved bool, pinTo string,
) (int, error) {
	refApplyTree, err := sc.tree(ctx, s.dirs.refApply)
	if err != nil {
		return 0, err
	}
	m := merger{dbDir: o.dbDir, work: s.work, baseDir: s.dirs.base, refDir: s.dirs.ref, theirsDir: s.dirs.refApply}
	changes, err := sc.changes(ctx, baseTree, refApplyTree)
	if err != nil {
		return 0, err
	}
	report, err := m.apply(ctx, changes)
	if err != nil {
		return 0, err
	}
	if pinMoved {
		if err = setHarnessTestDataCommit(o.dbDir, pinTo); err != nil {
			return 0, err
		}
		report.notes = append(report.notes, fmt.Sprintf(
			"TestDataCommit in %s set to %s; update the Test data row of UPSTREAM.md and db/tests/Readme.md",
			testDataCommitFile, pinTo))
	}
	fmt.Fprintf(w, "\n== 3. Apply ==\n")
	report.print(w)
	if s.base != s.ref {
		fmt.Fprintf(w, "\nNext: update the Base table in UPSTREAM.md to %s, then rerun with -ref %s: "+
			"it must report no upstream changes.\n", s.ref, o.ref)
	}
	return len(report.conflicts), nil
}

func printDiff(ctx context.Context, w io.Writer, sc *scratch, from, to string, statOnly bool) error {
	if from == to {
		fmt.Fprintln(w, "(none)")
		return nil
	}
	stat, err := sc.diff(ctx, from, to, "-M", "--stat=200")
	if err != nil {
		return err
	}
	if _, err = w.Write(stat); err != nil {
		return err
	}
	if statOnly {
		return nil
	}
	patch, err := sc.diff(ctx, from, to, "-M")
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	_, err = w.Write(patch)
	return err
}
