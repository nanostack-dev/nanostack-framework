package pipeline

import (
	"os"
)

// scanOrphans reports, or without Check deletes, each file whose header
// names a step that ran without failing and that no unit of the run
// produces. It does not
// run when discovery was incomplete: a declaration in a file that did not
// parse, or behind a misspelled //nano: directive, would make its outputs
// look orphaned.
func (r *runner) scanOrphans() {
	if !r.found.complete {
		return
	}
	for _, h := range r.found.headers {
		// A failed step produced nothing, so its files cannot be told from
		// orphans: the db step plans its models only when it runs.
		if !r.ran[h.step] || r.failed[h.step] || r.planned[h.path] {
			continue
		}
		rel := r.rel(h.path)
		if r.opts.Check {
			r.files = append(r.files, Event{Step: h.step, Level: LevelOrphan, File: rel, Msg: "orphan " + rel,
				Fix: "run nanostack gen without -check, which deletes it, then commit the result"})
			continue
		}
		if err := os.Remove(h.path); err != nil {
			r.problem(h.step, "", fileError(h.path, err))
			continue
		}
		r.files = append(r.files, Event{Step: h.step, Level: LevelDeleted, File: rel, Msg: "deleted " + rel})
	}
}
