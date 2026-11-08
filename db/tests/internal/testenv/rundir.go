package testenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// Per-run directories (jet binary, generated model trees, SQLite copies, working dirs)
// live under one stable root, not in a fresh os.TempDir name, so a run killed by a
// test timeout or a panic, which skips the deferred cleanup, leaves a directory the
// next run finds and deletes.
const runDirName = "run"

// staleAfter bounds how long a run directory whose owner cannot be checked is kept.
const staleAfter = 24 * time.Hour

// pidFile records the process that owns a run directory.
const pidFile = "owner.pid"

// newRunDir creates the run directory of this process for suite, after deleting the
// directories of runs that are no longer alive.
func newRunDir(suite string) (string, error) {
	root, err := runRoot()
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(root, 0o750); err != nil {
		return "", err
	}
	removeStaleRunDirs(root, time.Now())

	dir, err := os.MkdirTemp(root, suite+"-")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, pidFile), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func runRoot() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir: %w", err)
	}
	return filepath.Join(cache, "nanostack-db", runDirName), nil
}

// removeStaleRunDirs deletes every run directory under root whose owner process is
// gone, or that is older than staleAfter when the owner cannot be checked.
func removeStaleRunDirs(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if runDirAlive(dir, now) {
			continue
		}
		_ = os.RemoveAll(dir)
	}
}

func runDirAlive(dir string, now time.Time) bool {
	data, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err == nil {
		if pid, atoiErr := strconv.Atoi(strings.TrimSpace(string(data))); atoiErr == nil {
			switch processAlive(pid) {
			case aliveYes:
				return true
			case aliveNo:
				return false
			default: // aliveUnknown: decide by age below
			}
		}
	}
	// No owner recorded (a run still creating its directory) or unknowable: keep it
	// until it is clearly abandoned.
	info, err := os.Stat(dir)
	return err == nil && now.Sub(info.ModTime()) < staleAfter
}

type aliveness int

const (
	aliveUnknown aliveness = iota
	aliveYes
	aliveNo
)

// processAlive checks pid with signal 0 where the platform supports it.
func processAlive(pid int) aliveness {
	if pid == os.Getpid() {
		return aliveYes
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return aliveNo
	}
	err = p.Signal(syscall.Signal(0))
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return aliveYes
	case errors.Is(err, os.ErrProcessDone), errors.Is(err, syscall.ESRCH):
		return aliveNo
	}
	return aliveUnknown
}

// requireRyuk fails when Testcontainers' reaper is disabled. The harness stops its
// containers in a deferred call, which a test timeout or a panic skips; Ryuk then
// removes them when the test process exits. Without it every such run leaks containers.
func requireRyuk() error {
	if testcontainers.ReadConfig().Config.RyukDisabled {
		return errors.New(
			"the Testcontainers Ryuk reaper is disabled (TESTCONTAINERS_RYUK_DISABLED or ryuk.disabled in ~/.testcontainers.properties); " +
				"the harness needs it to remove the containers of a run that times out or panics. Enable it and run again",
		)
	}
	return nil
}
