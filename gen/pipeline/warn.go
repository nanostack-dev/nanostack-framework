package pipeline

import (
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// warn records each warning as a warning event of step, with relative
// paths resolved from base and reported relative to the root.
func (r *runner) warn(step, base string, warnings []*gen.Error) {
	for _, w := range warnings {
		file := w.File
		if file != "" {
			if !filepath.IsAbs(file) && base != "" {
				file = filepath.Join(base, file)
			}
			file = r.rel(file)
		}
		r.notes = append(r.notes, Event{Step: step, Level: LevelWarning, File: file, Line: w.Line, Msg: w.Msg,
			Fix: w.Fix})
	}
}
