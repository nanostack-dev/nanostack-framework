package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Main runs `nano gen [-dir d] [-check [-diff]] [-only s,s | -skip s,s]
// [-list] [-json]` and returns the exit code: 0 on success, 1 on a source
// error, a validation gap or a stale, missing or orphaned file, 2 on bad
// usage, 3 when the environment fails; -h prints the flags and returns 0.
// It is a gen.Command.
//
// A clean run prints nothing. Otherwise it prints one line per file (wrote,
// deleted, stale, missing, orphan), then each problem as "file:line: msg"
// with an indented "fix:" line. With -json it prints one Event per line on
// stdout instead.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nano gen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	var only, skip string
	var list, asJSON bool
	fs.StringVar(&opts.Dir, "dir", "",
		"directory to generate: nanogen/project.go sits in it (default: the module root)")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 listing every stale, missing or orphaned file")
	fs.BoolVar(&opts.Diff, "diff", false, "with -check: print the first lines of each diff")
	fs.StringVar(&only, "only", "", "comma-separated steps to run, in pipeline order: "+strings.Join(StepNames(), ","))
	fs.StringVar(&skip, "skip", "", "comma-separated steps not to run")
	fs.BoolVar(&list, "list", false, "print the plan: step, package, inputs, outputs; run nothing")
	fs.BoolVar(&asJSON, "json", false, "print one JSON event per line: {step, level, file, line, msg, fix}")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(
			stderr,
			"nano gen: unexpected argument %q; generator names come before the flags\n",
			fs.Arg(0),
		)
		return 2
	}
	opts.Only, opts.Skip = splitList(only), splitList(skip)
	var res Result
	if list {
		res = List(opts)
	} else {
		res = Run(opts)
	}
	if asJSON {
		writeJSON(stdout, res.Events)
		return res.Exit
	}
	writeText(stdout, stderr, res.Events)
	if list && res.Exit != 2 {
		writeTemplate(stdout, opts.Dir)
	}
	return res.Exit
}

func splitList(s string) []string {
	var out []string
	for f := range strings.SplitSeq(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func writeJSON(w io.Writer, events []Event) {
	enc := json.NewEncoder(w)
	for _, e := range events {
		_ = enc.Encode(e)
	}
}

// writeText prints files written and deleted, and the plan, on stdout; every
// failure on stderr: the files first, then the problems.
func writeText(stdout, stderr io.Writer, events []Event) {
	failed := false
	for _, e := range events {
		switch e.Level {
		case LevelWrote, LevelDeleted:
			_, _ = fmt.Fprintln(stdout, e.Msg)
		case LevelPlan:
			writePlan(stdout, e)
		case LevelStale, LevelMissing, LevelOrphan:
			_, _ = fmt.Fprintln(stderr, e.Msg)
			for _, l := range e.Diff {
				_, _ = fmt.Fprintln(stderr, "    "+l)
			}
			failed = true
		}
	}
	if failed {
		_, _ = fmt.Fprintln(stderr, "  fix: run nano gen without -check, then commit the result")
	}
	for _, e := range events {
		switch e.Level {
		case LevelSkipped:
			_, _ = fmt.Fprintf(stderr, "nano gen %s: %s\n", e.Step, e.Msg)
		case LevelError:
			writeProblem(stderr, e)
		}
	}
}

func writePlan(w io.Writer, e Event) {
	_, _ = fmt.Fprintf(w, "%-9s %s\n", e.Step, e.File)
	for _, in := range e.Inputs {
		_, _ = fmt.Fprintf(w, "  in   %s\n", in)
	}
	for _, out := range e.Outputs {
		_, _ = fmt.Fprintf(w, "  out  %s\n", out)
	}
}

// writeProblem prints "file:line: msg", or "nano gen <step>: msg" without a
// file, then the fix line.
func writeProblem(w io.Writer, e Event) {
	ge := gen.Error{File: e.File, Line: e.Line, Msg: e.Msg, Fix: e.Fix}
	prefix := ""
	switch {
	case e.File != "":
	case e.Step == StepPlan:
		prefix = "nano gen: "
	default:
		prefix = "nano gen " + e.Step + ": "
	}
	_, _ = fmt.Fprintln(w, prefix+ge.Error())
}

// writeTemplate prints, when the directory has no nanogen/project.go, a
// declaration filled from the OpenAPI document and the server package it
// finds.
func writeTemplate(w io.Writer, dir string) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	root, err := resolveRoot(wd, dir)
	if err != nil {
		return
	}
	if _, statErr := os.Stat(filepath.Join(root, projectFile)); !errors.Is(statErr, os.ErrNotExist) {
		return
	}
	var b strings.Builder
	b.WriteString("\nno " + projectFile + "; nano gen runs the steps it discovers. To declare the inputs it cannot " +
		"discover, write " + projectFile + ":\n\n")
	b.WriteString("// Package nanogen declares the inputs of this module's code generation that\n" +
		"// cannot be discovered. Run: go tool nano gen\npackage nanogen\n\n" +
		"import \"" + projectPkg + "\"\n\n")
	b.WriteString("var Project = project.Project{")
	if specs := findSpecs(root); len(specs) > 0 {
		server := findServer(root)
		if server == "" {
			server = "<the oapi-codegen package directory>"
		}
		_, _ = fmt.Fprintf(
			&b,
			"\n\tAPI: &project.OpenAPI{\n\t\tSpec:   %q,\n\t\tServer: project.Package{Dir: %q},\n\t},\n",
			specs[0],
			server,
		)
	}
	b.WriteString("}\n")
	_, _ = io.WriteString(w, b.String())
}

// findServer returns the directory, relative to root, of the first package
// that declares an oapi-codegen StrictServerInterface, or "".
func findServer(root string) string {
	found := ""
	_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		if e.IsDir() {
			if path != root && skipDir(path, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path) //nolint:gosec // a path under the root the caller walks
		if readErr == nil && bytes.Contains(src, []byte("type StrictServerInterface interface")) {
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			found = filepath.ToSlash(rel)
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
