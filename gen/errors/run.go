// Package generrors implements `nano gen errors`: it reads a module's error
// declarations, a Go interface annotated with //nano:errors, and writes typed
// error constructors (errors.gen.go) and a Markdown catalogue (ERRORS.md).
// ADR 0022 records why the source is a Go interface rather than errors.yaml.
//
// A declaration file looks like this:
//
//	package billingerr
//
//	import "time"
//
//	// declarations is the billing error catalogue.
//	//
//	//nano:errors namespace=billing
//	type declarations interface {
//		// No invoice has the requested id.
//		//
//		//nano:error code=invoice_not_found status=404 title="Invoice not found"
//		InvoiceNotFound(id string)
//
//		// The invoice was paid already and cannot be paid again.
//		//
//		//nano:error code=invoice_already_paid status=409 title="Invoice is already paid" detail="Invoice {id} was paid at {paidAt}."
//		InvoiceAlreadyPaid(id string, paidAt time.Time)
//	}
//
// Each method becomes a constructor with the same signature, returning a typed
// error (InvoiceAlreadyPaidError) whose fields are the parameters, a Kind
// sentinel (ErrInvoiceAlreadyPaid) and an entry in the Kinds list.
//
// The stable code is <namespace>.<code> and the problem type URI is
// <type_base>/<namespace>/<code>. Both are the client contract, so code= is
// required on every error: renaming the Go method changes call sites only,
// never the contract. Changing a code is a breaking API change; the
// committed ERRORS.md and `nano gen errors -check` in CI make it visible in
// review. The generator checks that codes are unique within one namespace;
// it cannot see other packages, so keep namespaces distinct per service.
//
// The generator type-checks the declaration package with go/packages, so
// parameter types may come from any package, under any import name. It
// rejects names that would clash in the generated file, and it type-checks
// the rendered file before returning it: a declaration never produces code
// that does not compile, and every failure names the declaration's
// file:line.
//
// The package is named generrors rather than errors so that it never shadows
// the standard library inside the generator.
package generrors

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Options configures one run.
type Options struct {
	// Dir is the package directory that holds the declaration file.
	Dir string
	// Out is the generated Go file name inside Dir. Default: errors.gen.go.
	Out string
	// Doc is the Markdown catalogue file name inside Dir. Default: ERRORS.md.
	Doc string
	// Check compares instead of writing and returns an error wrapping
	// gen.ErrStale on drift.
	Check bool
	// Overlay replaces files in the package loads, keyed by absolute path,
	// as in golang.org/x/tools/go/packages. The output file is always
	// replaced: by an empty file, then by the new output for the final
	// type check.
	Overlay map[string][]byte
}

func (o Options) withDefaults() Options {
	if o.Dir == "" {
		o.Dir = "."
	}
	if o.Out == "" {
		o.Out = "errors.gen.go"
	}
	if o.Doc == "" {
		o.Doc = "ERRORS.md"
	}
	return o
}

// Generate returns the generated files without touching the disk.
func Generate(opts Options) ([]gen.File, error) {
	opts = opts.withDefaults()
	l, c, err := parseDir(opts)
	if err != nil {
		return nil, err
	}
	src, err := renderGo(c, l.scopeNames())
	if err != nil {
		return nil, err
	}
	if err = l.verify(c, src); err != nil {
		return nil, err
	}
	return []gen.File{
		{Path: filepath.Join(opts.Dir, opts.Out), Data: src},
		{Path: filepath.Join(opts.Dir, opts.Doc), Data: renderMarkdown(c)},
	}, nil
}

// Run generates the files and writes those that changed. With Check it writes
// nothing and returns an error wrapping gen.ErrStale that names every file
// that would change.
func Run(opts Options) error {
	_, err := run(opts)
	return err
}

func run(opts Options) ([]string, error) {
	files, err := Generate(opts)
	if err != nil {
		return nil, err
	}
	return gen.Sync("errors", files, opts.Check)
}

// Main runs `nano gen errors [-dir d] [-out f] [-doc f] [-check]` and returns
// the process exit code: 0 on success, 1 on stale output or a declaration
// error, 2 on bad flags, 3 when the environment fails; -h prints the flags
// and returns 0. It is a gen.Command.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nano gen errors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	fs.StringVar(&opts.Dir, "dir", ".", "package directory holding the //nano:errors declaration")
	fs.StringVar(&opts.Out, "out", "errors.gen.go", "generated Go file name inside -dir")
	fs.StringVar(&opts.Doc, "doc", "ERRORS.md", "generated Markdown catalogue name inside -dir")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 when generated files are stale")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	written, err := run(opts)
	if err != nil {
		return gen.Report(stderr, "errors", err)
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nano gen errors: wrote %s\n", path)
	}
	return 0
}
