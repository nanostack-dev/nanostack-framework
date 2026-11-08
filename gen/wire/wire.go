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
	// Overlay replaces files in every package load, keyed by absolute
	// path, as in golang.org/x/tools/go/packages.
	Overlay map[string][]byte
}

func (o Options) resolve() (Options, error) {
	if o.Dir == "" {
		o.Dir = "."
	}
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return o, gen.EnvErrorf("run from a directory that exists", "resolve %s: %v", o.Dir, err)
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
			return o, gen.EnvErrorf("run from a directory that exists", "find the working directory: %v", err)
		}
	}
	if o.Base, err = filepath.Abs(o.Base); err != nil {
		return o, gen.EnvErrorf("pass a Base directory that exists", "resolve %s: %v", o.Base, err)
	}
	return o, nil
}

// Generate loads the manifest package and returns wire.gen.go, without
// touching the disk. Graph problems are returned as one *gen.Error each,
// joined, in discovery order, with paths relative to Options.Base.
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
// 2 on bad flags, 3 when the environment fails; -h prints the flags and
// returns 0. It is a gen.Command.
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
		return gen.Report(stderr, "wire", err)
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nano gen wire: wrote %s\n", path)
	}
	return 0
}

// pos is a source position for generator errors, relative to Options.Base.
// The zero pos is unknown.
type pos struct {
	file string
	line int
}

// diags collects the problems of the manifest and the provider graph,
// without duplicates, in discovery order.
type diags struct {
	list []error
	seen map[gen.Error]bool
}

func (d *diags) add(at pos, fix, format string, args ...any) {
	e := gen.Errorf(at.file, at.line, fix, format, args...)
	if d.seen == nil {
		d.seen = map[gen.Error]bool{}
	}
	if d.seen[*e] {
		return
	}
	d.seen[*e] = true
	d.list = append(d.list, e)
}

func (d *diags) err() error {
	return errors.Join(d.list...)
}
