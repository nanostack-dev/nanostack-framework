// Command nano is the Nanostack command line. Today it runs code generators:
//
//	nano gen <generator> [flags]
//
// Each generator follows the contract of package gen and documents its flags
// (nano gen <generator> -h). Every generator takes -check, which writes
// nothing and exits 1 when its output is stale.
package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	genconfig "github.com/nanostack-dev/nanostack-framework/gen/config"
	generrors "github.com/nanostack-dev/nanostack-framework/gen/errors"
	genvalidate "github.com/nanostack-dev/nanostack-framework/gen/validate"
	"github.com/nanostack-dev/nanostack-framework/gen/wire"
)

// generators maps `nano gen <name>` to its command.
var generators = map[string]gen.Command{
	"config":   genconfig.Main,
	"errors":   generrors.Main,
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
	return "usage: nano gen {" + strings.Join(names, ",") + "} [flags]"
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "gen" {
		_, _ = fmt.Fprintln(stderr, usage())
		return 2
	}
	cmd, ok := generators[args[1]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "nano gen: unknown generator %q\n%s\n", args[1], usage())
		return 2
	}
	return cmd(args[2:], stdout, stderr)
}
