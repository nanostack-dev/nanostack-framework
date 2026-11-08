package testenv

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// DiffModels compares the generated directories of s under wantRoot/.gentestdata and
// gotRoot/.gentestdata. It returns a readable report, or "" when they are identical; the
// report shows the content diff of the first maxDiffs differing files.
func DiffModels(s Source, wantRoot, gotRoot string, maxDiffs int) (string, error) {
	var report strings.Builder
	shown := 0

	for _, dir := range ModelDirs(s) {
		want, err := readTree(filepath.Join(wantRoot, GenTestDataDir, dir))
		if err != nil {
			return "", err
		}
		got, err := readTree(filepath.Join(gotRoot, GenTestDataDir, dir))
		if err != nil {
			return "", err
		}

		names := map[string]bool{}
		for name := range want {
			names[name] = true
		}
		for name := range got {
			names[name] = true
		}
		sorted := make([]string, 0, len(names))
		for name := range names {
			sorted = append(sorted, name)
		}
		sort.Strings(sorted)

		for _, name := range sorted {
			path := filepath.Join(GenTestDataDir, dir, name)
			w, inWant := want[name]
			g, inGot := got[name]
			switch {
			case !inGot:
				fmt.Fprintf(&report, "  only in the committed models: %s\n", path)
			case !inWant:
				fmt.Fprintf(&report, "  only in the generator output: %s\n", path)
			case !bytes.Equal(w, g):
				fmt.Fprintf(&report, "  differs: %s\n", path)
				if shown < maxDiffs {
					shown++
					fmt.Fprintf(&report, "%s\n", indent(cmp.Diff(string(w), string(g)), "    "))
				}
			}
		}
	}
	return report.String(), nil
}

// readTree reads every file under dir, keyed by slash-separated relative path.
// A missing dir reads as empty.
func readTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == dir {
				return filepath.SkipDir
			}
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// G304, G122: the trees are generated or committed test models the harness owns.
		data, err := os.ReadFile(path) //nolint:gosec // see above
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
