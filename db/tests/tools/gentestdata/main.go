// Command gentestdata regenerates the committed test models in db/tests/.gentestdata
// with this repository's generator, against the same containers the suites use.
//
//	go run ./tools/gentestdata                       # refresh the committed models
//	go run ./tools/gentestdata -dialects all -out d  # write each source's output to d/<source>
//
// It generates into a temp dir first and replaces a model directory only when the
// generator wrote files for it, so a broken database or test data cache can never
// empty the committed tree.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

func main() {
	dialects := flag.String("dialects", "", "sources to generate from (default: the committed ones)")
	out := flag.String("out", "", "write each source's models to <out>/<source> instead of the tree")
	flag.Parse()

	if err := run(*dialects, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gentestdata:", err)
		os.Exit(1)
	}
}

func run(dialects, out string) error {
	sources := testenv.CommittedSources
	if dialects != "" {
		if err := os.Setenv(testenv.DialectsEnv, dialects); err != nil {
			return err
		}
		var err error
		if sources, err = testenv.Selected(); err != nil {
			return err
		}
	}
	if out == "" {
		for _, s := range sources {
			if !testenv.IsCommitted(s) {
				return fmt.Errorf("%s models are not committed; use -out to inspect them", s)
			}
		}
	}

	dataDir, err := testenv.TestData()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "nanodb-gentestdata-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	for _, s := range sources {
		root := repo.GetTestsDirPath()
		if out != "" {
			root = filepath.Join(out, string(s))
		}
		fmt.Fprintf(os.Stdout, "gentestdata: %s -> %s\n", s, filepath.Join(root, testenv.GenTestDataDir))
		staging := filepath.Join(tmp, "out-"+string(s))
		if err = generate(s, dataDir, filepath.Join(tmp, "sqlite-"+string(s)), staging); err != nil {
			return err
		}
		if err = testenv.ReplaceModels(s, staging, root); err != nil {
			return err
		}
	}
	return nil
}

func generate(s testenv.Source, dataDir, sqliteDir, root string) error {
	if s == testenv.SQLite {
		// The generator opens the files read-write; never point it at the shared cache.
		if err := testenv.CopySQLite(dataDir, sqliteDir); err != nil {
			return err
		}
		return testenv.GenerateModels(s, nil, sqliteDir, root)
	}

	d, err := testenv.Start(context.Background(), s, dataDir)
	if err != nil {
		return err
	}
	defer d.Close()
	return testenv.GenerateModels(s, d, "", root)
}
