// Command upstreamfmt keeps `golangci-lint fmt` away from the files taken from go-jet.
//
// Files taken from go-jet keep upstream's style, so an upstream diff stays small (UPSTREAM.md,
// rules 1 and 2). golangci-lint's formatters ignore linters.exclusions.paths-except, and
// formatters.exclusions has only paths, so db/.golangci.local.yml lists the go-jet files there.
// This command derives that list from the go-jet header of each file: one pattern for each
// directory tree that holds only go-jet files, and one pattern for the go-jet files of each
// directory that also holds Nanostack files. Run it from the db directory:
//
//	go run ./tools/upstreamfmt          # fails when the list is stale (CI)
//	go run ./tools/upstreamfmt -write   # rewrites the list
//
// After -write, regenerate db/.golangci.yml from the repository root:
// `go run ./lint/cmd/lintconfig -dir db`.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	// header starts the modification header that every file taken from go-jet carries.
	header = "// Modified by Nanostack from go-jet"
	// headerLines is how far into a file the header may sit.
	headerLines = 5
	configFile  = ".golangci.local.yml"
	beginMarker = "# BEGIN upstreamfmt"
	endMarker   = "# END upstreamfmt"
	indent      = "    "
)

func main() {
	dir := flag.String("db", ".", "path to the db module root")
	write := flag.Bool("write", false, "rewrite the list in "+configFile)
	flag.Parse()
	if err := run(*dir, *write); err != nil {
		fmt.Fprintln(os.Stderr, "upstreamfmt:", err)
		os.Exit(1)
	}
}

var errStale = errors.New(configFile + " lists stale formatter exclusions; run `go run ./tools/upstreamfmt -write`, " +
	"then `go run ./lint/cmd/lintconfig -dir db` from the repository root")

func run(dir string, write bool) error {
	upstream, nanostack, err := classify(dir)
	if err != nil {
		return err
	}
	block := render(patterns(upstream, nanostack))
	file := filepath.Join(dir, configFile)
	config, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	updated, err := replaceBlock(config, block)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, config) {
		return nil
	}
	if !write {
		return errStale
	}
	return os.WriteFile(file, updated, 0o600)
}

// classify returns the Go files under dir, as slash paths relative to dir, split into the
// files that carry the go-jet header and the others.
func classify(dir string) (upstream, nanostack []string, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		fromJet, readErr := hasHeader(p)
		if readErr != nil {
			return readErr
		}
		if fromJet {
			upstream = append(upstream, filepath.ToSlash(rel))
		} else {
			nanostack = append(nanostack, filepath.ToSlash(rel))
		}
		return nil
	})
	return upstream, nanostack, err
}

func hasHeader(file string) (bool, error) {
	f, err := os.Open(file)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for i := 0; i < headerLines && scanner.Scan(); i++ {
		if strings.HasPrefix(scanner.Text(), header) {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// patterns returns the exclusion patterns that match every upstream file and no Nanostack
// file: `^dir/` for the highest directories whose tree holds only upstream Go files, and
// `^dir/(a|b)\.go$` for the upstream files of a directory that also holds Nanostack files.
func patterns(upstream, nanostack []string) []string {
	mixed := map[string]bool{} // directories with a Nanostack file somewhere below
	for _, f := range nanostack {
		for d := path.Dir(f); ; d = path.Dir(d) {
			mixed[d] = true
			if d == "." {
				break
			}
		}
	}
	trees := map[string]bool{}
	files := map[string][]string{}
	for _, f := range upstream {
		dir := path.Dir(f)
		if mixed[dir] {
			files[dir] = append(files[dir], strings.TrimSuffix(path.Base(f), ".go"))
			continue
		}
		top := dir // the highest ancestor without a Nanostack file
		for parent := path.Dir(top); parent != "." && !mixed[parent]; parent = path.Dir(parent) {
			top = parent
		}
		trees[top] = true
	}
	var out []string
	for dir := range trees {
		out = append(out, "^"+regexp.QuoteMeta(dir)+"/")
	}
	for dir, names := range files {
		slices.Sort(names)
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = regexp.QuoteMeta(n)
		}
		prefix := "^"
		if dir != "." {
			prefix += regexp.QuoteMeta(dir) + "/"
		}
		out = append(out, prefix+"("+strings.Join(quoted, "|")+`)\.go$`)
	}
	slices.Sort(out)
	return out
}

func render(patterns []string) []byte {
	var b bytes.Buffer
	b.WriteString(indent + "paths:\n")
	for _, p := range patterns {
		b.WriteString(indent + "  - " + p + "\n")
	}
	return b.Bytes()
}

// replaceBlock replaces the lines between the begin and end markers of config with block.
func replaceBlock(config, block []byte) ([]byte, error) {
	begin := bytes.Index(config, []byte(beginMarker))
	end := bytes.Index(config, []byte(endMarker))
	if begin < 0 || end < begin {
		return nil, fmt.Errorf("%s has no %q ... %q block", configFile, beginMarker, endMarker)
	}
	afterBegin := begin + bytes.IndexByte(config[begin:], '\n') + 1
	lineStart := bytes.LastIndexByte(config[:end], '\n') + 1
	out := slices.Concat(config[:afterBegin], block, config[lineStart:])
	return out, nil
}
