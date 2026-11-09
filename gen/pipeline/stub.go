package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// stubBody replaces every function body of a stub.
const stubBody = ` panic("nano gen stub") `

// stub returns src with every function body replaced by a panic. Every
// declaration stays, so code that calls the file's functions still
// type-checks, while a body that reads something renamed since no longer
// breaks the load. An import that only the bodies used becomes a blank
// import. ok is false when src does not parse; the file is then left as it
// is.
func stub(src []byte) ([]byte, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	type edit struct {
		from, to int
		text     string
	}
	var edits []edit
	var bodies []*ast.BlockStmt
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			bodies = append(bodies, fn.Body)
			edits = append(edits, edit{
				from: fset.Position(fn.Body.Lbrace).Offset + 1,
				to:   fset.Position(fn.Body.Rbrace).Offset,
				text: stubBody,
			})
		}
	}
	inside, outside := selectorNames(f, bodies)
	for _, imp := range f.Imports {
		name := gopkg.ImportName(imp)
		if name == "_" || name == "." || outside[name] || !inside[name] {
			// Kept: still used, or used under a name the guess missed.
			continue
		}
		e := edit{from: fset.Position(imp.Path.Pos()).Offset, text: "_ "}
		e.to = e.from
		if imp.Name != nil {
			e.from = fset.Position(imp.Name.Pos()).Offset
			e.to = fset.Position(imp.Name.End()).Offset
			e.text = "_"
		}
		edits = append(edits, e)
	}
	slices.SortFunc(edits, func(a, b edit) int { return b.from - a.from })
	out := slices.Clone(src)
	for _, e := range edits {
		out = slices.Concat(out[:e.from], []byte(e.text), out[e.to:])
	}
	return out, true
}

// selectorNames returns the identifiers used as the left side of a selector
// inside the bodies and outside them.
func selectorNames(f *ast.File, bodies []*ast.BlockStmt) (inside, outside map[string]bool) {
	inside, outside = map[string]bool{}, map[string]bool{}
	isBody := map[ast.Node]bool{}
	for _, b := range bodies {
		isBody[b] = true
		ast.Inspect(b, func(n ast.Node) bool {
			if id := selectorX(n); id != nil {
				inside[id.Name] = true
			}
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if isBody[n] {
			return false
		}
		if id := selectorX(n); id != nil {
			outside[id.Name] = true
		}
		return true
	})
	return inside, outside
}

// selectorX returns the identifier x of a selector x.y, or nil.
func selectorX(n ast.Node) *ast.Ident {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	id, _ := sel.X.(*ast.Ident)
	return id
}

// overlay returns the files that replace the disk in unit i's package
// loads: the fresh outputs of earlier units under -check, and a stub of each
// Go output of a later unit: its step's forward stub, or else the
// body-stripped stub of the file on disk.
func (r *runner) overlay(i int) map[string][]byte {
	ov := map[string][]byte{}
	if r.opts.Check {
		for path, data := range r.fresh {
			if strings.HasSuffix(path, ".go") && dirExists(filepath.Dir(path)) {
				ov[path] = data
			}
		}
	}
	for _, later := range r.units[i+1:] {
		r.stubOutputs(later, ov)
	}
	return ov
}

// stubOutputs adds the stubs of the outputs of later, a unit that has not
// run yet, to ov.
func (r *runner) stubOutputs(later *unit, ov map[string][]byte) {
	var forward map[string][]byte
	if later.step.forward != nil {
		forward = later.step.forward(later)
	}
	for path, src := range forward {
		if _, done := r.fresh[path]; !done {
			ov[path] = src
		}
	}
	for _, out := range later.outputs {
		_, done := r.fresh[out]
		_, stubbed := forward[out]
		if done || stubbed || !strings.HasSuffix(out, ".go") {
			continue
		}
		src, err := os.ReadFile(out)
		if err != nil {
			continue // missing: nothing to replace
		}
		if s, ok := stub(src); ok {
			ov[out] = s
		}
	}
}

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
