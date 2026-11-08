package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/utils/repo"
)

// The suites read SQL dumps, SQLite files and expected results from go-jet's
// jet-test-data repository. It has no license, so it is never committed here:
// the harness fetches the pinned commit into the user cache on first use.
const (
	TestDataRepo   = "https://github.com/go-jet/jet-test-data"
	TestDataCommit = "7527e38f8290db3ef359eb1426ff49015528ccd0"

	// TestDataEnv overrides the cache with an existing checkout.
	TestDataEnv = "NANODB_TESTDATA"
)

// TestData returns the test data directory and links db/tests/testdata to it,
// fetching the pinned commit into the cache when needed.
func TestData() (string, error) {
	dir, err := resolveTestData()
	if err != nil {
		return "", err
	}
	if err = linkTestData(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func resolveTestData() (string, error) {
	if override := os.Getenv(TestDataEnv); override != "" {
		dir, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if err = checkLayout(dir); err != nil {
			return "", fmt.Errorf("%s=%s: %w", TestDataEnv, override, err)
		}
		if head, headErr := gitHead(dir); headErr == nil && head != TestDataCommit {
			fmt.Fprintf(
				os.Stderr,
				"testenv: %s is at commit %s, the pinned commit is %s\n",
				TestDataEnv,
				head,
				TestDataCommit,
			)
		}
		if dirty, dirtyErr := gitDirty(dir); dirtyErr == nil && dirty != "" {
			fmt.Fprintf(os.Stderr, "testenv: %s has local changes:\n%s", TestDataEnv, dirty)
		}
		return dir, nil
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir (set %s to use a local checkout): %w", TestDataEnv, err)
	}
	dir := filepath.Join(cacheRoot, "nanostack-db", "testdata", TestDataCommit)

	if verifyCheckout(dir) == nil {
		return dir, nil
	}
	if err = fetchTestData(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// fetchTestData clones the pinned commit next to dir and renames it into place, so
// concurrent test processes never see a partial checkout.
func fetchTestData(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		if verifyCheckout(dir) == nil {
			return nil // another test process fetched it meanwhile
		}
		// A broken, wrong or locally changed checkout from an earlier run: replace it.
		fmt.Fprintf(os.Stderr, "testenv: replacing the test data cache %s\n", dir)
		if err = os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove stale test data %s: %w", dir, err)
		}
	}

	tmp, err := os.MkdirTemp(filepath.Dir(dir), TestDataCommit[:12]+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	fmt.Fprintf(os.Stderr, "testenv: fetching %s@%s into %s\n", TestDataRepo, TestDataCommit[:12], dir)
	steps := [][]string{
		{"init", "--quiet"},
		{"fetch", "--quiet", "--depth=1", TestDataRepo, TestDataCommit},
		{"-c", "advice.detachedHead=false", "checkout", "--quiet", "FETCH_HEAD"},
	}
	for _, args := range steps {
		if out, gitErr := git(tmp, args...); gitErr != nil {
			return fmt.Errorf("fetch test data: git %s: %w\n%s", strings.Join(args, " "), gitErr, out)
		}
	}
	if err = verifyCheckout(tmp); err != nil {
		return fmt.Errorf("fetch test data: %w", err)
	}

	if err = os.Rename(tmp, dir); err != nil {
		// Another test process finished first.
		if verifyCheckout(dir) == nil {
			return nil
		}
		return fmt.Errorf("move test data into %s: %w", dir, err)
	}
	return nil
}

// verifyCheckout checks that dir is a git checkout of exactly the pinned commit with no
// local change to a tracked file. HEAD names the commit, not the working tree: a golden
// file rewritten by SaveJSONFile or a truncated SQLite file keeps HEAD unchanged, and the
// cache lives outside the repository, so nothing else would show the change.
func verifyCheckout(dir string) error {
	return verifyCheckoutAt(dir, TestDataCommit)
}

func verifyCheckoutAt(dir, commit string) error {
	head, err := gitHead(dir)
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("test data in %s is at %s, want %s", dir, head, commit)
	}
	dirty, err := gitDirty(dir)
	if err != nil {
		return err
	}
	if dirty != "" {
		return fmt.Errorf("test data in %s has local changes:\n%s", dir, dirty)
	}
	return checkLayout(dir)
}

// gitDirty returns `git status --porcelain` for the tracked files of dir, empty when
// the working tree matches HEAD. Untracked files are ignored: the suites only read
// files the commit has.
func gitDirty(dir string) (string, error) {
	out, err := git(dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", fmt.Errorf("git status in %s: %w\n%s", dir, err, out)
	}
	return out, nil
}

func checkLayout(dir string) error {
	for _, sub := range []string{"init/postgres", "init/cockroach", "init/mysql", "init/sqlite", "results"} {
		// G703: dir is the test data checkout (NANODB_TESTDATA or the user cache).
		if fi, err := os.Stat(filepath.Join(dir, sub)); err != nil || !fi.IsDir() { //nolint:gosec // see above
			return fmt.Errorf("missing %s directory", sub)
		}
	}
	return nil
}

func gitHead(dir string) (string, error) {
	// G703: dir is the test data checkout (NANODB_TESTDATA or the user cache).
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil { //nolint:gosec // see above
		return "", err
	}
	out, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse in %s: %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// linkTestData makes db/tests/testdata a symlink to dir. The suites read expected
// results through that path (db/internal/testutils resolves it from the package dir).
func linkTestData(dir string) error {
	link := repo.GetTestsFilePath("testdata")

	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&os.ModeSymlink != 0:
		if target, linkErr := os.Readlink(link); linkErr == nil && target == dir {
			return nil
		}
	case fi.IsDir():
		// The empty placeholder left by upstream's git submodule.
		if err = os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"%s is a non-empty directory, remove it so the harness can link the test data: %w",
				link,
				err,
			)
		}
	default:
		return fmt.Errorf("%s exists and is not a symlink", link)
	}

	tmp := fmt.Sprintf("%s.tmp-%d", link, os.Getpid())
	_ = os.Remove(tmp)
	if err = os.Symlink(dir, tmp); err != nil {
		return fmt.Errorf("link test data: %w", err)
	}
	if err = os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("link test data: %w", err)
	}
	return nil
}
