// Package gen is the contract every Nanostack generator follows, so that
// cmd/nano routes `nano gen <name>` to any of them the same way. A generator
// package exposes:
//
//   - Options, the configuration of one run, with a Check field: compare
//     instead of writing.
//   - Generate(Options) ([]gen.File, error): render in memory, touch nothing.
//     gen/validate is the one variation: its Generate returns a *Result
//     that holds the gen.File and the schema features it does not enforce.
//   - Run(Options) error: write the files that changed with Sync; with Check,
//     write nothing and return an error wrapping ErrStale.
//   - Main(args []string, stdout, stderr io.Writer) int, a Command: parse
//     flags with ParseFlags, call Run, report. Exit 0 on success or after
//     -h, 1 on a source error or stale output, 2 on bad usage. Main writes each error once, after the
//     prefix "nano gen <name>: ". Generate, Run and Sync add no such prefix.
//
// The subpackages oapi and jet hold conventions for third-party generators
// and are not commands.
package gen

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// File is one generated output, not yet written.
type File struct {
	Path string // absolute, or relative to the working directory
	Data []byte
}

// Command is a generator's command-line entry point, as cmd/nano calls it
// with the arguments after `nano gen <name>`.
type Command func(args []string, stdout, stderr io.Writer) int

// ParseFlags parses args into fs, which must use flag.ContinueOnError and
// write to the command's stderr. ok is true when the run should go on.
// Otherwise exit is the code Main returns: 0 after -h or -help, which printed
// the usage as asked, and 2 on bad usage, which the flag package already
// reported.
func ParseFlags(fs *flag.FlagSet, args []string) (exit int, ok bool) {
	err := fs.Parse(args)
	switch {
	case err == nil:
		return 0, true
	case errors.Is(err, flag.ErrHelp):
		return 0, false
	default:
		return 2, false
	}
}

// ErrStale reports that generated files differ from what their source
// produces: the failure of a Check run. Sync returns it as a *StaleError.
var ErrStale = errors.New("generated files are stale")

// StaleError is the error of a Check run that found missing or differing
// files. errors.Is(err, ErrStale) holds for it.
type StaleError struct {
	Name  string   // the generator, as in `nano gen <name>`
	Files []string // every missing or differing file, in the order given to Sync
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("%v: %s; run nano gen %s", ErrStale, strings.Join(e.Files, ", "), e.Name)
}

// Is reports whether target is ErrStale.
func (e *StaleError) Is(target error) bool { return target == ErrStale }

// Sync writes each file whose content differs from the disk, creating its
// directory, and returns the paths it wrote. With check it writes nothing and
// returns a *StaleError that names every missing or differing file and tells
// the reader to run `nano gen <name>`.
//
// Other errors come from the file system and name the path; they carry no
// generator prefix, because the caller's Main adds it once.
func Sync(name string, files []File, check bool) ([]string, error) {
	var changed []string
	for _, f := range files {
		have, readErr := os.ReadFile(f.Path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		if readErr == nil && bytes.Equal(have, f.Data) {
			continue
		}
		changed = append(changed, f.Path)
		if check {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o750); err != nil {
			return nil, err
		}
		//nolint:gosec // generated source is committed and readable like any source file
		if err := os.WriteFile(f.Path, f.Data, 0o644); err != nil {
			return nil, err
		}
	}
	if check && len(changed) > 0 {
		return nil, &StaleError{Name: name, Files: changed}
	}
	return changed, nil
}
