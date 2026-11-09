package genrepo

import (
	"fmt"
	"go/ast"
	"go/types"
)

// resolveMethod resolves one method of the interface: a method that takes
// a declared search, a hand-written or promoted method, a derived name, or
// an error.
func (r *resolver) resolveMethod(d *declarations, fn *types.Func, hand map[string]*ast.FuncDecl) {
	s := r.searchOf(d, fn)
	if s != nil {
		r.res.takes = append(r.res.takes, s.name+"Search")
	}
	switch {
	case s != nil && hand[fn.Name()] != nil:
		r.checkHandSearch(fn, hand[fn.Name()], s)
		file, line := r.l.position(r.l.pkg.Fset, hand[fn.Name()].Pos())
		r.line(fn, fmt.Sprintf("hand-written %s:%d, runs search %s", file, line, s.name), "")
	case s != nil && len(r.promoted(fn.Name())) > 0:
		h := r.promoted(fn.Name())[0]
		r.fail(fn.Pos(), fn, fmt.Sprintf("write %s on *%s by hand on searchexec.Resolve, or remove it from %s "+
			"so nanostack gen repo generates it", fn.Name(), r.b.impl.Obj().Name(), h.field.Name()),
			"%s takes a %sSearch and is promoted from the embedded %s: nanostack gen repo cannot check that it "+
				"runs the declared scope", fn.Name(), s.name, h.field.Name())
		r.line(fn, "ERROR: a promoted method takes a search", "")
	case s != nil:
		r.resolveSearch(fn, s)
	case invalidSig(fn.Type()):
		r.res.incomplete = true
		r.line(fn, "ERROR: the signature does not type-check", "")
	case hand[fn.Name()] != nil:
		r.resolveHandWritten(fn, hand[fn.Name()])
	default:
		if hits := r.promoted(fn.Name()); len(hits) > 0 {
			r.resolvePromoted(fn, hits)
			return
		}
		m, fail := r.derive(fn)
		if fail != nil {
			r.unimplemented(fn, fail)
			return
		}
		r.res.derived = append(r.res.derived, m)
		how := "derived: "
		if m.unscoped {
			how = "derived, UNSCOPED (Internal): "
		}
		r.line(fn, how+r.methodSQL(m), "")
	}
}
