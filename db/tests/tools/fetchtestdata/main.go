// Command fetchtestdata fetches the pinned go-jet/jet-test-data commit into the user
// cache (or reuses a clean checkout there) and links db/tests/testdata to it.
//
//	go run ./tools/fetchtestdata
//
// Run it once before building the suites: they import the Go package
// testdata/results/common, which exists only inside the test data, and the compiler
// needs it before any TestMain can run. The test data has no license, so it is never
// committed; see db/tests/Readme.md.
package main

import (
	"fmt"
	"os"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/testenv"
)

func main() {
	dir, err := testenv.TestData()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetchtestdata:", err)
		os.Exit(1)
	}
	fmt.Fprintf(
		os.Stdout,
		"fetchtestdata: db/tests/testdata -> %s (%s@%s)\n",
		dir,
		testenv.TestDataRepo,
		testenv.TestDataCommit[:12],
	)
}
