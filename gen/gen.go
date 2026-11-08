// Package gen is the contract every Nanostack generator follows, so that
// gen/cmd/nano routes `nano gen <name>` to any of them the same way. A generator
// package exposes:
//
//   - Options, the configuration of one run, with a Check field (compare
//     instead of writing) and an Overlay field (file contents that replace
//     the disk in every go/packages load, keyed by absolute path).
//   - Generate(Options) ([]gen.File, error): render in memory, touch nothing.
//     gen/validate is the one variation: its Generate returns a *Result
//     that holds the gen.File and the schema features it does not enforce.
//   - Run(Options) error: write the files that changed with Sync; with Check,
//     write nothing and return an error wrapping ErrStale.
//   - Main(args []string, stdout, stderr io.Writer) int, a Command: parse
//     flags with ParseFlags, call Run, and print the result with Report.
//
// Every error a generator returns is an *Error, or several joined with
// errors.Join, and every *Error has a Fix. Sync's *StaleError is the one
// other error; its fix is to run the generator. Exit codes: 0 on success or
// after -h, 1 on a source error or stale output, 2 on bad usage, 3 when the
// environment fails (an *Error that wraps ErrEnvironment: the go command
// cannot load packages, or a file cannot be read or written). Report writes
// each error once: "file:line: msg" when it has a file, "nano gen <name>: msg"
// when it has none, then an indented "fix:" line. Generate, Run and Sync add
// no prefix.
//
// gen/api is the one generator whose Options have no Overlay: it reads the
// spec and loads no Go package. The subpackage jet holds go-jet's
// conventions and is not a command.
package gen

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File is one generated output, not yet written.
type File struct {
	Path string // absolute, or relative to the working directory
	Data []byte
}

// Command is a generator's command-line entry point, as gen/cmd/nano calls it
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
// Other errors come from the file system: an *Error on the path that wraps
// ErrEnvironment, so Main exits 3.
func Sync(name string, files []File, check bool) ([]string, error) {
	var changed []string
	for _, f := range files {
		have, readErr := os.ReadFile(f.Path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, fileError(f.Path, readErr)
		}
		if readErr == nil && bytes.Equal(have, f.Data) {
			continue
		}
		changed = append(changed, f.Path)
		if check {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o750); err != nil {
			return nil, fileError(f.Path, err)
		}
		//nolint:gosec // generated source is committed and readable like any source file
		if err := os.WriteFile(f.Path, f.Data, 0o644); err != nil {
			return nil, fileError(f.Path, err)
		}
	}
	if check && len(changed) > 0 {
		return nil, &StaleError{Name: name, Files: changed}
	}
	return changed, nil
}

// fileError reports a file the generator cannot read or write.
func fileError(path string, err error) *Error {
	msg := err.Error()
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		msg = pe.Op + ": " + pe.Err.Error() // the path is File already
	}
	return &Error{
		File: path,
		Msg:  msg,
		Fix:  "make the path a writable regular file, or remove what is in its place",
		Err:  ErrEnvironment,
	}
}

// ErrEnvironment marks an *Error that the machine caused, not the source: the
// go command cannot load packages, or a file cannot be read or written. Main
// exits 3 on it, so a caller can tell "my code is wrong" (exit 1) from "this
// machine cannot run the generator".
var ErrEnvironment = errors.New("environment failure")

// Error is one problem a generator found. Every generator error is an
// *Error, and every *Error has a Fix: a problem without a fix is a bug in the
// generator. Several problems are returned joined with errors.Join, in the
// order the generator found them.
type Error struct {
	// File is the file the problem is in, relative to the directory the
	// generator reports from (its package directory or the working
	// directory). Empty when no file applies.
	File string
	// Line is the 1-based line in File. 0 when the problem concerns the
	// whole file or no line is known, such as a JSON Pointer into a spec.
	Line int
	// Msg says what is wrong. It does not repeat File or Line.
	Msg string
	// Fix says what to change.
	Fix string
	// Err classifies the problem for errors.Is, such as ErrEnvironment. It
	// is not printed.
	Err error
}

// Errorf returns a source problem at file:line. The fix comes before the
// message, so that no call can leave it out.
func Errorf(file string, line int, fix, format string, args ...any) *Error {
	return &Error{File: file, Line: line, Msg: fmt.Sprintf(format, args...), Fix: fix}
}

// EnvErrorf returns a problem of the environment, with no file. It wraps
// ErrEnvironment.
func EnvErrorf(fix, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Fix: fix, Err: ErrEnvironment}
}

// Error returns "file:line: msg" (or "file: msg", or "msg" alone), then a
// second line "  fix: <fix>".
func (e *Error) Error() string {
	var b strings.Builder
	switch {
	case e.File != "" && e.Line > 0:
		_, _ = fmt.Fprintf(&b, "%s:%d: ", e.File, e.Line)
	case e.File != "":
		b.WriteString(e.File + ": ")
	}
	b.WriteString(e.Msg)
	if e.Fix != "" {
		b.WriteString("\n  fix: " + e.Fix)
	}
	return b.String()
}

// Unwrap returns the classification, such as ErrEnvironment.
func (e *Error) Unwrap() error { return e.Err }

// Errors flattens err into its problems, in order: each *Error once, and
// each errors.Join branch in turn. A *StaleError becomes one problem whose
// fix is to run the generator. Any other error is returned as an *Error
// without a Fix and with the error as Err; Report flags it, and the
// generator tests fail on it.
func Errors(err error) []*Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok { //nolint:errorlint // a wrapped *Error would lose its wrapper's text
		return []*Error{e}
	}
	if s, ok := err.(*StaleError); ok { //nolint:errorlint // same
		msg := fmt.Sprintf("%v: %s", ErrStale, strings.Join(s.Files, ", "))
		return []*Error{{Msg: msg, Fix: "run nano gen " + s.Name, Err: ErrStale}}
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok { // errors.Join, walked branch by branch
		var out []*Error
		for _, e := range j.Unwrap() {
			out = append(out, Errors(e)...)
		}
		return out
	}
	return []*Error{{Msg: err.Error(), Err: err}}
}

// ExitCode returns the exit code for the result of a run: 0 for nil, 3 when
// any problem wraps ErrEnvironment, 1 otherwise.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	for _, e := range Errors(err) {
		if errors.Is(e, ErrEnvironment) {
			return 3
		}
	}
	return 1
}

// Report writes each problem in err to w and returns the exit code. A
// problem with a file prints as "file:line: msg"; one without prints as
// "nano gen <name>: msg". A "  fix: <fix>" line follows each. A problem
// without a fix is a generator bug, and the fix line says so.
func Report(w io.Writer, name string, err error) int {
	for _, e := range Errors(err) {
		line := *e
		if line.Fix == "" {
			line.Fix = "none known; an error without a fix is a nano gen bug, report it"
		}
		prefix := ""
		if line.File == "" {
			prefix = "nano gen " + name + ": "
		}
		_, _ = fmt.Fprintln(w, prefix+line.Error())
	}
	return ExitCode(err)
}
