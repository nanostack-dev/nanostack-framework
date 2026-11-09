package genvalidate

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/oas"
)

// ErrUnenforced classifies the *gen.Error of a schema feature that the
// generated validators do not enforce and that no x-nano-unenforced
// extension accepts, and of an extension that accepts no gap. A validation
// gap must not ship unnoticed, so it fails the run; the file is still
// written.
var ErrUnenforced = errors.New("schema feature is not enforced")

// Run generates the validators and writes the file when it changed. With
// Options.Check it writes nothing and returns an error wrapping gen.ErrStale
// when the file is missing or differs. It also returns the Result's Gaps;
// the file is still written then, and lists every gap.
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
	return res, written, errors.Join(syncErr, res.Gaps())
}

// Main runs `nanostack gen validate -spec file [-dir dir] [-out file] [-check]`
// and returns the exit code: 0 on success, 1 on a source error, stale output
// or an unaccepted validation gap, 2 on bad flags, 3 when the environment
// fails; -h prints the flags and returns 0. It is a gen.Command.
//
// A schema feature the validators do not enforce is a failure: a validation
// gap must not ship unnoticed. The generated file is still written, listing
// the gap. Accept a gap with the x-nano-unenforced extension on the schema,
// whose value is the reason; it covers that schema and every schema under
// it:
//
//	Shape:
//	  x-nano-unenforced: "the handler checks the variant"
//	  oneOf: [...]
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nanostack gen validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	fs.StringVar(&opts.Spec, "spec", "", "OpenAPI 3.0 or 3.1 document")
	fs.StringVar(&opts.Dir, "dir", ".", "directory of the oapi-codegen package")
	fs.StringVar(&opts.Output, "out", DefaultOutput, "generated file name inside -dir")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 if the generated file is stale")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	if opts.Spec == "" {
		_, _ = fmt.Fprintln(stderr, "nanostack gen validate: -spec is required")
		return 2
	}
	res, written, err := run(opts)
	if res != nil {
		for _, d := range res.Diagnostics {
			if d.Waived != "" {
				_, _ = fmt.Fprintf(stderr, "not enforced (%s: %s): %s\n", oas.WaiverKey, commentText(d.Waived), d)
			}
		}
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nanostack gen validate: wrote %s\n", path)
	}
	return gen.Report(stderr, "validate", err)
}
