// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package repo

import (
	"path/filepath"
	"runtime"
)

// GetRootDirPath will return the root dir of the db module (the parent of the tests folder).
//
// Nanostack: upstream asked git for the repository top level, which is the framework
// root here, not the db module. The path is now derived from this source file.
func GetRootDirPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("repo: cannot locate source file")
	}

	// file is <root>/tests/internal/utils/repo/repo.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

// GetTestsDirPath will return tests folder full path
func GetTestsDirPath() string {
	return filepath.Join(GetRootDirPath(), "tests")
}

// GetTestsFilePath will return full file path of the file in the tests folder
func GetTestsFilePath(subPath string) string {
	return filepath.Join(GetTestsDirPath(), subPath)
}

// GetTestDataFilePath will return full file path of the file in the testdata folder
func GetTestDataFilePath(subPath string) string {
	return filepath.Join(GetTestsDirPath(), "testdata", subPath)
}
