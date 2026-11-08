// Package genconfig implements `nano gen config`: it reads an annotated Go
// configuration struct and writes a reflection-free binder, a redacted
// LogValue/String, a Markdown reference of every setting and a .env.example.
//
// Annotations are struct tags; the field's doc comment is its description:
//
//	type Config struct {
//		Billing BillingConfig `config:"billing"`
//	}
//
//	type BillingConfig struct {
//		// Days before an open invoice is overdue.
//		DueDays int `config:"due_days" default:"30" validate:"min=1,max=365"`
//		// Sender address for invoice mail.
//		From string `config:"from" env:"BILLING_FROM"`
//		// Stripe API key.
//		StripeKey secret.Secret `config:"stripe_key" env:"STRIPE_KEY"`
//		// Extra recipients of overdue notices.
//		CC []string `config:"cc,optional"`
//	}
//
// Tags:
//
//   - config:"name[,optional]" names the setting (lower snake case). Nested
//     structs add a path segment: billing.due_days. Every exported field needs
//     one; config:"-" skips a field. Embedded structs without a tag are inlined.
//   - env:"NAME" overrides the environment variable, derived from the path by
//     default (BILLING_DUE_DAYS). On a nested struct it sets the prefix.
//   - default:"value" is parsed and validated at generation time. A setting
//     without a default is required unless marked optional. Secrets cannot
//     have one. Defaults of types implementing encoding.TextUnmarshaler (such
//     as slog.Level) are the exception: only their UnmarshalText can parse
//     them, so they are checked when Bind runs, and a bad one fails every
//     start-up. Bind the defaults in a test of the configuration package to
//     catch that before deploying.
//   - defaultenv:"NAME" takes the default from another environment variable
//     first: the setting reads NAME only when its own env and the config
//     files leave it unset, then default. One variable so feeds two settings,
//     as the env of one and the default of the other. NAME must not be the
//     setting's own env nor a secret setting's env; secrets and nested
//     structs take none.
//   - validate:"rule,rule" with min=, max= (numbers, durations), oneof=a b c
//     (strings, integers) and scheme=https http (URLs). On a slice the rules
//     apply to every element.
//
// Supported types: strings, bools, integers, floats (and named types of
// those), time.Duration, *url.URL, secret.Secret, any type whose pointer
// implements encoding.TextUnmarshaler, slices of those, and nested structs.
//
// The package loads with -tags=nanogen, as nano gen wire does: wire.gen.go
// and the files that call Build carry //go:build !nanogen, so a renamed
// Config field they still read never blocks regeneration. Keep Config in a
// file without a build tag.
//
// The package is named genconfig rather than config so that it never collides
// with the core/config runtime it generates calls to.
package genconfig

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Options configures one run.
type Options struct {
	// Dir is the package directory that declares the root type.
	Dir string
	// Type is the root configuration struct. Default: Config.
	Type string
	// Out is the generated Go file name inside Dir. Default: config.gen.go.
	Out string
	// Doc is the Markdown reference file name inside Dir. Default: CONFIG.md.
	Doc string
	// EnvExample is the .env.example path, relative to Dir. Default:
	// .env.example.
	EnvExample string
	// Check compares instead of writing and returns an error wrapping
	// gen.ErrStale on drift.
	Check bool
	// Overlay replaces files in the package load, keyed by absolute path,
	// as in golang.org/x/tools/go/packages. The output file is always
	// replaced by a stub of the generated API.
	Overlay map[string][]byte
}

func (o Options) withDefaults() Options {
	if o.Dir == "" {
		o.Dir = "."
	}
	if o.Type == "" {
		o.Type = "Config"
	}
	if o.Out == "" {
		o.Out = "config.gen.go"
	}
	if o.Doc == "" {
		o.Doc = "CONFIG.md"
	}
	if o.EnvExample == "" {
		o.EnvExample = ".env.example"
	}
	return o
}

// Generate returns the generated files without touching the disk.
func Generate(opts Options) ([]gen.File, error) {
	opts = opts.withDefaults()
	m, err := load(opts)
	if err != nil {
		return nil, err
	}
	src, err := renderGo(m)
	if err != nil {
		return nil, err
	}
	return []gen.File{
		{Path: filepath.Join(opts.Dir, opts.Out), Data: src},
		{Path: filepath.Join(opts.Dir, opts.Doc), Data: renderMarkdown(m)},
		{Path: filepath.Join(opts.Dir, opts.EnvExample), Data: renderEnvExample(m)},
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
	return gen.Sync("config", files, opts.Check)
}

// Main runs `nano gen config [-dir d] [-type T] [-out f] [-doc f] [-env f] [-check]`
// and returns the process exit code: 0 on success, 1 on stale output or a
// source error, 2 on bad flags, 3 when the environment fails; -h prints the
// flags and returns 0. It is a gen.Command.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nano gen config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	fs.StringVar(&opts.Dir, "dir", ".", "package directory declaring the configuration struct")
	fs.StringVar(&opts.Type, "type", "Config", "root configuration struct")
	fs.StringVar(&opts.Out, "out", "config.gen.go", "generated Go file name inside -dir")
	fs.StringVar(&opts.Doc, "doc", "CONFIG.md", "generated Markdown reference name inside -dir")
	fs.StringVar(&opts.EnvExample, "env", ".env.example", "generated .env.example path, relative to -dir")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 when generated files are stale")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	written, err := run(opts)
	if err != nil {
		return gen.Report(stderr, "config", err)
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nano gen config: wrote %s\n", path)
	}
	return 0
}
