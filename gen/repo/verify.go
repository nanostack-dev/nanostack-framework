package genrepo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// VerifyResult is the outcome of VerifyScopes.
type VerifyResult struct {
	// Methods is the number of derived methods whose code was read: a
	// function declaration found in a generated file, not a comment.
	Methods int
	// Violations lists each derived method whose code does not cover every
	// scope column of its table, or whose guard is missing.
	Violations []string
}

// VerifyScopes is the mechanical proof that no derived method omits a scope
// column. It resolves the package as Generate does, reads the derived files
// that are on disk, and for each derived method checks the code that runs,
// not the declarations:
//   - the scope columns of its table are the columns of the table package
//     that opts.Scopes names, read from the table files;
//   - a read or a delete covers each of them by a predicate (r.where.X), by
//     the registered function (on r.t.X), or by a column that a covered one
//     implies through Postgres.ScopeImplies, an entry that its table proves
//     with a foreign key;
//   - a method marked UNSCOPED skips exactly the columns its comment names;
//   - a create stamps each scope column into the row, an update has each in
//     its WHERE, an upsert stamps each and has each in its conflict target
//     or the guard of its DO UPDATE, none through an implication;
//   - a delete or a write on a table whose context scope has a Present
//     function checks it first.
func VerifyScopes(opts Options) (*VerifyResult, error) {
	res, err := Generate(opts)
	if err != nil {
		return nil, err
	}
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(res.dir, name)) }
	return verifyFiles(res.resolutions, opts, read)
}

// verifyFiles checks the derived files that read returns, by file name.
func verifyFiles(res []*resolution, opts Options, read func(name string) ([]byte, error)) (*VerifyResult, error) {
	out := &VerifyResult{}
	for _, r := range res {
		name := derivedFile(r.b.file)
		src, err := read(name)
		if err != nil {
			return nil, fmt.Errorf("read the derived file of %s: %w", r.b.iface.Obj().Name(), err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		v := &verifier{r: r, opts: opts}
		for _, m := range r.derived {
			fd := v.find(f, m.fn.Name())
			if fd == nil {
				out.Violations = append(out.Violations, v.name(m)+": no such method in "+name)
				continue
			}
			out.Methods++
			for _, bad := range v.method(m, fd) {
				out.Violations = append(out.Violations, v.name(m)+": "+bad)
			}
		}
	}
	return out, nil
}

type verifier struct {
	r    *resolution
	opts Options
}

func (v *verifier) name(m *derivedMethod) string {
	return v.r.b.iface.Obj().Name() + "." + m.fn.Name()
}

// find returns the method of the implementation of the binding in f.
func (v *verifier) find(f *ast.File, name string) *ast.FuncDecl {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || fd.Name.Name != name {
			continue
		}
		typ := fd.Recv.List[0].Type
		if star, isStar := typ.(*ast.StarExpr); isStar {
			typ = star.X
		}
		if id, isID := typ.(*ast.Ident); isID && id.Name == v.r.b.impl.Obj().Name() {
			return fd
		}
	}
	return nil
}

// scopeFields are the Go fields of the scope columns of the table, read from
// the table package.
func (v *verifier) scopeFields() []string {
	var out []string
	for _, c := range v.r.b.table.columns {
		if slices.Contains(v.opts.Scopes, c.sqlName) {
			out = append(out, c.field)
		}
	}
	return out
}

// closure adds to covered the scope fields that a covered one implies, each
// step an entry of ScopeImplies that the table proves with a foreign key.
func (v *verifier) closure(covered map[string]bool) {
	t := v.r.b.table
	for changed := true; changed; {
		changed = false
		for _, c := range t.columns {
			next, ok := v.opts.ScopeImplies[c.sqlName]
			if !ok || !covered[c.field] {
				continue
			}
			if !t.proves(c.sqlName, next) {
				continue
			}
			for _, d := range t.columns {
				if d.sqlName == next && !covered[d.field] {
					covered[d.field], changed = true, true
				}
			}
		}
	}
}

// codeFacts are what the code of one derived method does with the scope.
type codeFacts struct {
	stamped    map[string]bool // fields set on the row: row.OrgID = orgID
	covered    map[string]bool // fields in a predicate, a function call or a conflict target
	hasPresent map[string]bool // functions called as if !f(ctx)
}

// scan reads the facts of fd.
func scan(fd *ast.FuncDecl) codeFacts {
	f := codeFacts{map[string]bool{}, map[string]bool{}, map[string]bool{}}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt: // row.OrgID = orgID, row.WorkspaceID, err = Stamp(ctx)
			for _, lhs := range n.Lhs {
				if field := assignedField(lhs); field != "" {
					f.stamped[field] = true
				}
			}
		case *ast.SelectorExpr:
			// r.where.OrgID, r.t.WorkspaceID, r.itemsBase.t.OrgID: the field after where or t.
			if inner, isInner := n.X.(*ast.SelectorExpr); isInner &&
				(inner.Sel.Name == "where" || inner.Sel.Name == "t") {
				f.covered[n.Sel.Name] = true
			}
		case *ast.IfStmt: // if !pkg.Present(ctx) { ... }
			if name := negatedCall(n.Cond); name != "" {
				f.hasPresent[name] = true
			}
		}
		return true
	})
	return f
}

// assignedField returns F for the expression row.F, else "".
func assignedField(e ast.Expr) string {
	if sel, isSel := e.(*ast.SelectorExpr); isSel {
		if id, isID := sel.X.(*ast.Ident); isID && id.Name == "row" {
			return sel.Sel.Name
		}
	}
	return ""
}

// negatedCall returns the function name of !f(...), else "".
func negatedCall(cond ast.Expr) string {
	if un, isUn := cond.(*ast.UnaryExpr); isUn && un.Op == token.NOT {
		if call, isCall := un.X.(*ast.CallExpr); isCall {
			return funcName(call.Fun)
		}
	}
	return ""
}

// method checks one derived method.
func (v *verifier) method(m *derivedMethod, fd *ast.FuncDecl) []string {
	f := scan(fd)
	var out []string
	if m.op.isWrite() {
		out = v.checkWrite(m, f)
	} else {
		out = v.checkRead(m, fd, f)
	}
	if m.op.changesRows() {
		out = append(out, v.checkPresent(f)...)
	}
	return out
}

// checkWrite checks a create, an update or an upsert: no scope column is left
// to the value, and no implication is used.
func (v *verifier) checkWrite(m *derivedMethod, f codeFacts) []string {
	var out []string
	for _, field := range v.scopeFields() {
		if m.op != opUpdate && !f.stamped[field] {
			out = append(out, "does not stamp "+columnOf(v.r.b.table, field)+": it would come from the value")
		}
		if m.op != opCreate && !f.covered[field] {
			out = append(out, "its "+writeWhere(m.op)+" leaves out "+columnOf(v.r.b.table, field))
		}
	}
	return out
}

// checkRead checks a read or a delete: the scope columns it leaves out are
// exactly the ones its Internal name skips.
func (v *verifier) checkRead(m *derivedMethod, fd *ast.FuncDecl, f codeFacts) []string {
	var out, missing []string
	v.closure(f.covered)
	for _, field := range v.scopeFields() {
		if !f.covered[field] {
			missing = append(missing, field)
		}
	}
	skipped := slices.Clone(m.skipped)
	slices.Sort(skipped)
	slices.Sort(missing)
	if !slices.Equal(missing, skipped) {
		out = append(out, fmt.Sprintf("its code leaves out [%s], and it is meant to skip [%s]",
			columnsOf(v.r.b.table, missing), columnsOf(v.r.b.table, skipped)))
	}
	if m.unscoped && !strings.HasSuffix(m.fn.Name(), "Internal") {
		out = append(out, "UNSCOPED without Internal")
	}
	if docHas(fd, "UNSCOPED:") != m.unscoped {
		out = append(out, "its UNSCOPED comment does not match its scope")
	}
	return out
}

// checkPresent checks that a delete or a write calls the Present function of
// each context scope of its table that has one.
func (v *verifier) checkPresent(f codeFacts) []string {
	var out []string
	for _, c := range v.r.b.table.columns {
		fn := v.opts.ContextPresents[c.sqlName]
		if fn != "" && v.opts.ContextScopes[c.sqlName] != "" && !f.hasPresent[identOfFunc(fn)] {
			out = append(out, "does not check "+fn+" first: a context without "+c.sqlName+" reaches every value")
		}
	}
	return out
}

func writeWhere(o op) string {
	if o == opUpsert {
		return "conflict target and DO UPDATE guard"
	}
	return "WHERE"
}

func columnsOf(t *tableBase, fields []string) string {
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = columnOf(t, f)
	}
	return strings.Join(cols, ", ")
}

// docHas reports whether the doc comment of fd has a line that starts with prefix.
func docHas(fd *ast.FuncDecl, prefix string) bool {
	if fd.Doc == nil {
		return false
	}
	for _, c := range fd.Doc.List {
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(c.Text, "//")), prefix) {
			return true
		}
	}
	return false
}

// funcName is the name of the function a call expression calls: Present for
// workspacescope.Present.
func funcName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// identOfFunc is the name after the last dot of a registered function.
func identOfFunc(fn string) string {
	_, ident, _ := strings.Cut(fn[strings.LastIndex(fn, "/")+1:], ".")
	return ident
}
