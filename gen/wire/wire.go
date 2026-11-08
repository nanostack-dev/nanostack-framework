// Package wire implements nano gen wire: it reads an app manifest (a
// nano.Manifest variable, see package core/nano), resolves the provider
// graph with go/packages and go/types, and writes wire.gen.go with
//
//	func Build(ctx context.Context, cfg Config, ov Overrides) (*nano.App, error)
//	func BuildComponents(ctx context.Context, cfg Config, ov Overrides) (*nano.App, *Components, error)
//
// Build calls the providers in dependency order, registers each value's
// lifecycle (Starter, Stopper, Runner, HealthChecker) with the App, and lets
// tests replace any provided value through the generated, typed Overrides.
// BuildComponents also returns every constructed value. Output is
// deterministic: the same inputs always produce the same bytes.
//
// The package follows the generator contract of package gen. The guide is
// docs/generation.md.
package wire

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// FileName is the name of the generated file.
const FileName = "wire.gen.go"

// DefaultOut is the output directory, relative to the manifest package.
const DefaultOut = "internal/gen/wire"

// Options configures one generation.
type Options struct {
	// Dir is the manifest package directory. Default ".".
	Dir string
	// Out is the output directory. Relative paths are resolved against Dir.
	// Default DefaultOut. Out may equal Dir to generate into the manifest
	// package itself.
	Out string
	// Tags are extra build tags for loading. The nanogen tag is always set.
	Tags []string
	// Base is the directory that diagnostic paths are relative to. Default
	// is the current working directory.
	Base string
	// Check makes Run compare instead of writing: it returns an error
	// wrapping gen.ErrStale when wire.gen.go is missing or differs.
	Check bool
}

func (o Options) resolve() (Options, error) {
	if o.Dir == "" {
		o.Dir = "."
	}
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return o, err
	}
	o.Dir = dir
	if o.Out == "" {
		o.Out = DefaultOut
	}
	if !filepath.IsAbs(o.Out) {
		o.Out = filepath.Join(o.Dir, o.Out)
	}
	if o.Base == "" {
		if o.Base, err = os.Getwd(); err != nil {
			return o, err
		}
	}
	if o.Base, err = filepath.Abs(o.Base); err != nil {
		return o, err
	}
	return o, nil
}

// Generate loads the manifest package and returns wire.gen.go, without
// touching the disk. A graph problem is reported as an *Error listing every
// diagnostic.
func Generate(opts Options) ([]gen.File, error) {
	opts, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	m, err := loadManifest(opts)
	if err != nil {
		return nil, err
	}
	g, err := resolve(m)
	if err != nil {
		return nil, err
	}
	src, err := emit(g)
	if err != nil {
		return nil, err
	}
	return []gen.File{{Path: filepath.Join(opts.Out, FileName), Data: src}}, nil
}

// Run generates wire.gen.go and writes it when it changed. With
// Options.Check it writes nothing and returns an error wrapping gen.ErrStale
// when the file is missing or differs.
func Run(opts Options) error {
	_, err := run(opts)
	return err
}

func run(opts Options) ([]string, error) {
	files, err := Generate(opts)
	if err != nil {
		return nil, err
	}
	return gen.Sync("wire", files, opts.Check)
}

// Main runs `nano gen wire [-dir dir] [-out dir] [-tags list] [-check]` and
// returns the exit code: 0 on success, 1 on a manifest error or stale output,
// 2 on bad flags; -h prints the flags and returns 0. It is a gen.Command.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nano gen wire", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	fs.StringVar(&opts.Dir, "dir", ".", "manifest package directory")
	fs.StringVar(&opts.Out, "out", DefaultOut, "output directory, relative to -dir")
	tags := fs.String("tags", "", "comma-separated extra build tags")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 if wire.gen.go is stale")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	if *tags != "" {
		opts.Tags = strings.Split(*tags, ",")
	}
	written, err := run(opts)
	if err != nil {
		if gerr, ok := errors.AsType[*Error](err); ok {
			for _, d := range gerr.Diagnostics {
				_, _ = fmt.Fprintln(stderr, d)
			}
		} else {
			_, _ = fmt.Fprintf(stderr, "nano gen wire: %v\n", err)
		}
		return 1
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nano gen wire: wrote %s\n", path)
	}
	return 0
}

// Diagnostic is one problem in the manifest or provider graph.
type Diagnostic struct {
	Pos string // file:line, relative to Options.Base; empty when unknown
	Msg string
}

func (d Diagnostic) String() string {
	if d.Pos == "" {
		return d.Msg
	}
	return d.Pos + ": " + d.Msg
}

// Error lists every diagnostic of a failed generation, in discovery order.
type Error struct {
	Diagnostics []Diagnostic
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		lines[i] = d.String()
	}
	return strings.Join(lines, "\n")
}

// diags collects diagnostics without duplicates.
type diags struct {
	list []Diagnostic
	seen map[Diagnostic]bool
}

func (d *diags) add(pos, format string, args ...any) {
	diag := Diagnostic{Pos: pos, Msg: fmt.Sprintf(format, args...)}
	if d.seen == nil {
		d.seen = map[Diagnostic]bool{}
	}
	if d.seen[diag] {
		return
	}
	d.seen[diag] = true
	d.list = append(d.list, diag)
}

func (d *diags) err() error {
	if len(d.list) == 0 {
		return nil
	}
	return &Error{Diagnostics: d.list}
}
