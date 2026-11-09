package genrepo

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// conditionHelpers are the base helpers that take a condition on the
// base's own table. The transactor methods (query, exec, page, ...) are left
// out: they take a whole statement, which may be an INSERT or read another
// table. A statement is judged by what it is built from instead: a where
// predicate, a SELECT, UPDATE or DELETE on b.t, or a SELECT that reads FROM
// b.t or a join on it.
var conditionHelpers = []string{ //nolint:gochecknoglobals // read-only table.
	"findOne", "first", "findAll", "exists", "count", "deleteWhere", "updateWhere",
}

// statementsOnTable are the methods of a table that start a statement
// reading or changing its rows: b.t.SELECT(...), and a join, which a
// SELECT reads FROM. INSERT is left out: a new row has no condition.
var statementsOnTable = []string{ //nolint:gochecknoglobals // read-only table.
	"SELECT", "UPDATE", "DELETE", "INNER_JOIN", "LEFT_JOIN", "RIGHT_JOIN", "FULL_JOIN", "CROSS_JOIN",
}

// scoped is a scoped table and the Go fields of its scope columns.
type scoped struct {
	t      *tableBase
	fields []string // in the table's column order
}

// scopedTables returns the tables of d that have a NOT NULL scope column.
func scopedTables(d *declarations, scopes []string) []scoped {
	var out []scoped
	for _, t := range d.tables {
		if fields := t.scopeFields(scopes); len(fields) > 0 {
			out = append(out, scoped{t: t, fields: fields})
		}
	}
	return out
}

// scopeWarnings runs the hand-written scope check (design D26) on pkg, the
// package type-checked with the new base: a hand-written function that
// touches a scoped table through its base, without a predicate or a
// reference on one of its scope columns, gets a warning, unless its name
// ends in Internal.
func (l *loaded) scopeWarnings(pkg *packages.Package, d *declarations, scopes []string) []*gen.Error {
	tables := scopedTables(d, scopes)
	if len(tables) == 0 {
		return nil
	}
	var out []*gen.Error
	for _, f := range pkg.Syntax {
		if own(f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || strings.HasSuffix(fd.Name.Name, "Internal") {
				continue
			}
			out = append(out, l.scopeOf(pkg, fd, tables)...)
		}
	}
	return out
}

// scopeOf checks one function.
func (l *loaded) scopeOf(pkg *packages.Package, fd *ast.FuncDecl, tables []scoped) []*gen.Error {
	touched := make([]bool, len(tables))
	filtered := make([]bool, len(tables))
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if s := pkg.TypesInfo.Selections[x]; s != nil {
				for i, st := range tables {
					t, f := st.uses(pkg, x, s)
					touched[i] = touched[i] || t
					filtered[i] = filtered[i] || f
				}
			}
		case *ast.CallExpr:
			for i, st := range tables {
				touched[i] = touched[i] || readsFrom(pkg, x, st.t)
			}
		}
		return true
	})
	var out []*gen.Error
	for i, st := range tables {
		if touched[i] && !filtered[i] {
			out = append(out, l.unscoped(pkg, fd, st))
		}
	}
	return out
}

// uses reports whether the selector sel, with selection s, touches the
// table of st through its base, and whether it filters on or refers to a
// scope column.
func (st scoped) uses(pkg *packages.Package, sel *ast.SelectorExpr, s *types.Selection) (touched, filtered bool) {
	recv := deref(s.Recv())
	switch {
	case isLocal(recv, st.t.whereName(), pkg):
		return true, slices.ContainsFunc(st.fields, func(f string) bool {
			return sel.Sel.Name == f || sel.Sel.Name == f+"In"
		})
	case s.Kind() == types.MethodVal && slices.Contains(conditionHelpers, sel.Sel.Name) &&
		embeds(recv, st.t.baseName(), pkg, 0):
		return true, false
	case slices.Contains(statementsOnTable, sel.Sel.Name) && onBaseTable(pkg, sel.X, st.t):
		return true, false
	case sameNamed(recv, deref(st.t.tableType)) && slices.Contains(st.fields, sel.Sel.Name):
		return false, true // a reference to the scope column, such as b.t.ProductID
	}
	return false, false
}

// readsFrom reports whether call is FROM(b.t), as in
// postgres.SELECT(...).FROM(r.t): a statement that reads the table of t.
func readsFrom(pkg *packages.Package, call *ast.CallExpr, t *tableBase) bool {
	fun, ok := call.Fun.(*ast.SelectorExpr)
	return ok && fun.Sel.Name == "FROM" && slices.ContainsFunc(call.Args, func(arg ast.Expr) bool {
		return onBaseTable(pkg, arg, t)
	})
}

// unscoped is the warning of a function that touches the table of st
// without its scope.
func (l *loaded) unscoped(pkg *packages.Package, fd *ast.FuncDecl, st scoped) *gen.Error {
	recv := "r"
	if fd.Recv != nil && len(fd.Recv.List) == 1 && len(fd.Recv.List[0].Names) == 1 {
		recv = fd.Recv.List[0].Names[0].Name
	}
	file, line := l.position(pkg.Fset, fd.Pos())
	cols := make([]string, len(st.fields))
	preds := make([]string, len(st.fields))
	for j, f := range st.fields {
		cols[j] = columnOf(st.t, f)
		preds[j] = recv + ".where." + f + "(…)"
	}
	return gen.Errorf(file, line,
		"add "+strings.Join(preds, " or ")+" to the condition, or name it "+fd.Name.Name+
			"Internal for a trusted system path",
		"%s: condition on %s has no %s; this query can read or change another tenant's rows",
		fd.Name.Name, st.t.sqlName, strings.Join(cols, " or "))
}

// sameNamed reports whether a and b are the same named type. The types may
// come from two loads of the package, so their objects differ.
func sameNamed(a, b types.Type) bool {
	x, okA := a.(*types.Named)
	y, okB := b.(*types.Named)
	return okA && okB && x.Obj().Pkg() != nil && y.Obj().Pkg() != nil &&
		x.Obj().Pkg().Path() == y.Obj().Pkg().Path() && x.Obj().Name() == y.Obj().Name()
}

func columnOf(t *tableBase, field string) string {
	for _, c := range t.columns {
		if c.field == field {
			return c.sqlName
		}
	}
	return field
}

func deref(t types.Type) types.Type {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		return types.Unalias(p.Elem())
	}
	return t
}

// isLocal reports whether t is the named type name of pkg.
func isLocal(t types.Type, name string, pkg *packages.Package) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() == pkg.Types && n.Obj().Name() == name
}

// embeds reports whether t is the base called name, or a struct that embeds
// it at any depth.
func embeds(t types.Type, name string, pkg *packages.Package, depth int) bool {
	if isLocal(t, name, pkg) {
		return true
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok || depth > 4 {
		return false
	}
	for f := range st.Fields() {
		if f.Embedded() && embeds(deref(f.Type()), name, pkg, depth+1) {
			return true
		}
	}
	return false
}

// onBaseTable reports whether x is the t field of the base of t, as in
// b.t or r.t.
func onBaseTable(pkg *packages.Package, x ast.Expr, t *tableBase) bool {
	inner, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "t" {
		return false
	}
	s := pkg.TypesInfo.Selections[inner]
	return s != nil && s.Kind() == types.FieldVal && embeds(deref(s.Recv()), t.baseName(), pkg, 0)
}
