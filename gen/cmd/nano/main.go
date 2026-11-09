// Command nano is the Nanostack command line. Today it runs code generators:
//
//	nano gen [flags]               # every step of the module, in order (package gen/pipeline)
//	nano gen <generator> [flags]   # one generator
//
// `nano gen` with no generator name runs the pipeline: it reads
// nanogen/project.go, discovers the rest, and runs db, api, validate,
// errors, repo, config and wire in that order; -check writes nothing and lists every stale,
// missing or orphaned file (nano gen -h lists the flags). Each generator
// follows the contract of package gen and documents its flags
// (nano gen <generator> -h); every generator takes -check, which writes
// nothing and exits 1 when its output is stale. Exit codes: 0 success, 1 a
// source error or stale output, 2 bad usage, 3 an environment failure (the
// go command cannot load packages, a file cannot be read or written, the
// binary links another oapi-codegen than the templates are cut from, or the
// db step needs a database and neither Docker nor NANO_GEN_DATABASE_URL
// gives one).
// Each error prints as "file:line: message" with an indented "fix:" line.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	genapi "github.com/nanostack-dev/nanostack-framework/gen/api"
	genconfig "github.com/nanostack-dev/nanostack-framework/gen/config"
	gendb "github.com/nanostack-dev/nanostack-framework/gen/db"
	generrors "github.com/nanostack-dev/nanostack-framework/gen/errors"
	"github.com/nanostack-dev/nanostack-framework/gen/pipeline"
	genrepo "github.com/nanostack-dev/nanostack-framework/gen/repo"
	genvalidate "github.com/nanostack-dev/nanostack-framework/gen/validate"
	"github.com/nanostack-dev/nanostack-framework/gen/wire"
)

// generators maps `nano gen <name>` to its command.
var generators = map[string]gen.Command{
	"api":      genapi.Main,
	"config":   genconfig.Main,
	"db":       gendb.Main,
	"errors":   generrors.Main,
	"repo":     genrepo.Main,
	"validate": genvalidate.Main,
	"wire":     wire.Main,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage() string {
	names := make([]string, 0, len(generators))
	for name := range generators {
		names = append(names, name)
	}
	slices.Sort(names)
	return "usage: nano gen [flags]\n       nano gen {" + strings.Join(names, ",") + "} [flags]"
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "gen" {
		_, _ = fmt.Fprintln(stderr, usage())
		return 2
	}
	if len(args) == 1 || strings.HasPrefix(args[1], "-") {
		return pipeline.Main(args[1:], stdout, stderr)
	}
	cmd, ok := generators[args[1]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "nano gen: unknown generator %q\n%s\n", args[1], usage())
		return 2
	}
	return cmd(args[2:], stdout, stderr)
}
