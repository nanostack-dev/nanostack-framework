package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

const jetCLIPackage = "github.com/nanostack-dev/nanostack-framework/db/cmd/jet"

// InstallJetCLI builds this repository's generator CLI into binDir and puts binDir
// first on PATH: the generator suites run the `jet` command, and must never pick up
// an upstream go-jet binary installed on the machine.
func InstallJetCLI(binDir string) error {
	name := "jet"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// go test puts its own GOROOT/bin first on the PATH of test binaries.
	bin := filepath.Join(binDir, name)
	cmd := exec.CommandContext(
		context.Background(),
		"go",
		"build",
		"-o",
		bin,
		jetCLIPackage,
	)

	cmd.Dir = repo.GetTestsDirPath()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s: %w\n%s", jetCLIPackage, err, out)
	}

	return os.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
