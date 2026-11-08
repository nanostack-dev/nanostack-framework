package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// stubBody replaces every function body of a stub.
const stubBody = ` panic("nano gen stub") `

var majorVersion = regexp.MustCompile(`^v[0-9]+$`) //nolint:gochecknoglobals // compiled once.

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
		name := importName(imp)
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

// importName returns the name an import binds: its explicit name, or a guess
// from its path (the last element, without a major version suffix).
func importName(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	p, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return ""
	}
	name := path.Base(p)
	if majorVersion.MatchString(name) && path.Dir(p) != "." {
		name = path.Base(path.Dir(p))
	}
	if i := strings.Index(name, ".v"); i > 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "go-")
	return strings.ReplaceAll(name, "-", "_")
}
