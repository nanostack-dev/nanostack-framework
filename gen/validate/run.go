package genvalidate

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// ErrUnenforced reports schema features that the generated validators do
// not enforce and that no Options.Allow location acknowledges. A validation
// gap must not ship unnoticed, so it fails the run.
var ErrUnenforced = errors.New("schema features are not enforced")

// Run generates the validators and writes the file when it changed. With
// Options.Check it writes nothing and returns an error wrapping gen.ErrStale
// when the file is missing or differs. It also returns an error wrapping
// ErrUnenforced when a diagnostic is not covered by Options.Allow; the file
// is still written then, and lists the gap.
func Run(opts Options) error {
	_, _, err := run(opts)
	return err
}

func run(opts Options) (*Result, []string, error) {
	res, err := Generate(opts)
	if err != nil {
		return nil, nil, err
	}
	written, syncErr := gen.Sync("validate", []gen.File{res.File}, opts.Check)
	unallowed := 0
	for _, d := range res.Diagnostics {
		if !allowed(d, opts.Allow) {
			unallowed++
		}
	}
	var gapErr error
	if unallowed > 0 {
		gapErr = fmt.Errorf("%w: %d; change the schema or acknowledge each location with -allow",
			ErrUnenforced, unallowed)
	}
	return res, written, errors.Join(syncErr, gapErr)
}

// allowed reports whether an allow location covers d: the schema itself or
// one of its ancestors.
func allowed(d Diagnostic, allow []string) bool {
	for _, loc := range allow {
		if d.Loc == loc || strings.HasPrefix(d.Loc, strings.TrimSuffix(loc, "/")+"/") {
			return true
		}
	}
	return false
}

// Main runs `nano gen validate -spec file [-dir dir] [-out file]
// [-allow pointer]... [-check]` and returns the exit code: 0 on success, 1 on
// a source error, stale output or an unacknowledged validation gap, 2 on bad
// flags; -h prints the flags and returns 0. It is a gen.Command.
//
// A schema feature the validators do not enforce is a failure: a validation
// gap must not ship unnoticed. The generated file is still written, listing
// the gap. Acknowledge a gap with -allow and the schema's JSON Pointer, as
// the message prints it; it covers that schema and every schema under it,
// and may be repeated:
//
//	nano gen validate -spec openapi.yaml -dir . -allow '#/components/schemas/Shape'
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nano gen validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	fs.StringVar(&opts.Spec, "spec", "", "OpenAPI 3.0 or 3.1 document")
	fs.StringVar(&opts.Dir, "dir", ".", "directory of the oapi-codegen package")
	fs.StringVar(&opts.Output, "out", DefaultOutput, "generated file name inside -dir")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 if the generated file is stale")
	fs.Func("allow", "JSON Pointer of a schema whose unenforced features are accepted (repeatable)",
		func(loc string) error {
			if !strings.HasPrefix(loc, "#/") {
				return fmt.Errorf("%q is not a JSON Pointer into the spec, such as #/components/schemas/X", loc)
			}
			opts.Allow = append(opts.Allow, loc)
			return nil
		})
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	if opts.Spec == "" {
		_, _ = fmt.Fprintln(stderr, "nano gen validate: -spec is required")
		return 2
	}
	res, written, err := run(opts)
	if res != nil {
		for _, d := range res.Diagnostics {
			if allowed(d, opts.Allow) {
				_, _ = fmt.Fprintln(stderr, "not enforced (allowed):", d)
			} else {
				_, _ = fmt.Fprintln(stderr, "not enforced:", d)
			}
		}
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nano gen validate: wrote %s\n", path)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "nano gen validate:", err)
		return 1
	}
	return 0
}
