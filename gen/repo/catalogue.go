package genrepo

import (
	"go/ast"
	"strings"
)

// foreignKey is one foreign key of a table, as nano gen db lists it in the
// ForeignKeys method of the table type.
type foreignKey struct {
	name       string   // constraint name
	columns    []string // referencing columns, in key order
	references string   // referenced table
	refColumns []string // referenced columns, in the order of columns
	refSchema  string   // schema of the referenced table when it differs, else ""
}

func (k foreignKey) String() string {
	ref := k.references
	if k.refSchema != "" {
		ref = k.refSchema + "." + ref
	}
	return k.name + " (" + strings.Join(k.columns, ", ") + ") -> " + ref + " (" + strings.Join(k.refColumns, ", ") +
		")"
}

// readForeignKeys reads each func (XTable) ForeignKeys() []postgres.ForeignKey
// of f into src, keyed by the table type. nano gen repo reads them from
// source and needs no database. A method is recorded even when it lists
// nothing, so a table with no foreign key is told apart from a table package
// that predates the method.
func readForeignKeys(f *ast.File, src *tableFiles) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "ForeignKeys" || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Body == nil {
			continue
		}
		if recv := identOf(fd.Recv.List[0].Type); recv != nil {
			src.foreignKeys[recv.Name] = foreignKeysOf(fd.Body)
		}
	}
}

// foreignKeysOf reads the {Name: ..., Columns: ..., References: ...,
// ReferencedColumns: ...} literals of a ForeignKeys body.
func foreignKeysOf(body *ast.BlockStmt) []foreignKey {
	keys := []foreignKey{}
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isKeyedLiteral(lit) {
			return true
		}
		var k foreignKey
		for _, e := range lit.Elts {
			kv, _ := e.(*ast.KeyValueExpr) // isKeyedLiteral checked every element
			switch identOf(kv.Key).Name {
			case "Name":
				k.name, _ = stringLit(kv.Value)
			case "Columns":
				k.columns = stringList(kv.Value)
			case "References":
				k.references, _ = stringLit(kv.Value)
			case "ReferencedColumns":
				k.refColumns = stringList(kv.Value)
			case "ReferencedSchema":
				k.refSchema, _ = stringLit(kv.Value)
			}
		}
		keys = append(keys, k)
		return false
	})
	return keys
}

// isKeyedLiteral reports whether every element of lit is Field: value.
func isKeyedLiteral(lit *ast.CompositeLit) bool {
	if len(lit.Elts) == 0 {
		return false
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok || identOf(kv.Key) == nil {
			return false
		}
	}
	return true
}

// stringList reads []string{"a", "b"}.
func stringList(e ast.Expr) []string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(lit.Elts))
	for _, el := range lit.Elts {
		if s, isString := stringLit(el); isString {
			out = append(out, s)
		}
	}
	return out
}
