package genconfig

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// fieldDoc returns the doc comment (or trailing line comment) of a struct
// field. Fields of other packages are found by parsing their source file at
// the position the type checker recorded.
func (l *loader) fieldDoc(f *types.Var) string {
	pos := l.fset.Position(f.Pos())
	if pos.Filename == "" {
		return ""
	}
	if file, ok := l.docs[pos.Filename]; ok {
		return docAtLine(file, l.fset, pos, f.Name())
	}
	file, ok := l.ext[pos.Filename]
	if !ok {
		parsed, err := parser.ParseFile(l.extFset, pos.Filename, nil, parser.ParseComments)
		if err != nil {
			parsed = nil
		}
		l.ext[pos.Filename], file = parsed, parsed
	}
	if file == nil {
		return ""
	}
	return docAtLine(file, l.extFset, pos, f.Name())
}

// docAtLine finds the field called name declared on pos's line.
func docAtLine(file *ast.File, fset *token.FileSet, pos token.Position, name string) string {
	var doc string
	ast.Inspect(file, func(n ast.Node) bool {
		field, ok := n.(*ast.Field)
		if doc != "" || !ok {
			return doc == ""
		}
		for _, id := range field.Names {
			if id.Name != name || fset.Position(id.Pos()).Line != pos.Line {
				continue
			}
			switch {
			case field.Doc != nil:
				doc = field.Doc.Text()
			case field.Comment != nil:
				doc = field.Comment.Text()
			}
		}
		return true
	})
	return strings.TrimSpace(doc)
}
