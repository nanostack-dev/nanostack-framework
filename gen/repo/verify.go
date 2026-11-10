package genrepo

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
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

// verifyOutput runs the scope-coverage check on the files that Generate is
// about to return, so that nanostack gen and its -check mode enforce it: a
// derived method whose code leaves a scope column to the value, or a Present
// guard out, is an error of the run.
func (l *loaded) verifyOutput(res []*resolution, opts Options, files []gen.File) error {
	read := func(name string) ([]byte, error) {
		for _, f := range files {
			if filepath.Base(f.Path) == name {
				return f.Data, nil
			}
		}
		return nil, fmt.Errorf("no generated file %s", name)
	}
	got, err := verifyFiles(res, opts, read)
	if err != nil {
		return err
	}
	var problems []error
	for _, bad := range got.Violations {
		problems = append(
			problems,
			gen.Errorf("", 0, "report this nanostack gen repo bug with the declaration that caused it",
				"scope coverage of the generated code: %s", bad),
		)
	}
	return errors.Join(problems...)
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
		v := &verifier{r: r, opts: opts, imports: importPaths(f)}
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
	r       *resolution
	opts    Options
	imports map[string]string // import name -> path, in the file under check
}

// importPaths maps the name each import of f gives to its path.
func importPaths(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = path
	}
	return out
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
	covered    map[string]bool // fields that a conjunct of a condition, or a conflict target, covers
	hasPresent map[string]bool // functions called as if !f(ctx), as import path and name
}

// derivedHelpers are the base helpers that take the condition of a derived
// read or delete as their second argument, after ctx.
var derivedHelpers = map[string]bool{ //nolint:gochecknoglobals // read-only table
	"findOne": true, "findAll": true, "exists": true, "count": true, "deleteWhere": true,
}

// scan reads the facts of fd. Only the statements at the top of the body
// count, and in them only the condition of the statement itself: a WHERE
// inside a nested SELECT, or a helper called inside a condition, credits
// nothing. A scope column is covered by a conjunct of that condition: the
// arguments of AND at its top, so that OR or NOT around a predicate covers
// nothing.
func (v *verifier) scan(fd *ast.FuncDecl) codeFacts {
	f := codeFacts{map[string]bool{}, map[string]bool{}, map[string]bool{}}
	stampedFields(fd, f.stamped)
	for _, stmt := range fd.Body.List {
		switch st := stmt.(type) {
		case *ast.ReturnStmt:
			for _, e := range st.Results {
				v.scanChain(e, f)
			}
		case *ast.AssignStmt:
			for _, e := range st.Rhs {
				v.scanChain(e, f)
			}
		case *ast.ExprStmt:
			v.scanChain(st.X, f)
		case *ast.IfStmt: // if !pkg.Present(ctx) { ... }
			if call := negatedCall(st.Cond); call != nil {
				f.hasPresent[v.resolve(call.Fun)] = true
			}
		}
	}
	return f
}

// stampedFields records the fields set on the row anywhere in fd:
// row.OrgID = orgID, row.WorkspaceID, err = Stamp(ctx).
func stampedFields(fd *ast.FuncDecl, into map[string]bool) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range as.Lhs {
				if field := assignedField(lhs); field != "" {
					into[field] = true
				}
			}
		}
		return true
	})
}

// scanChain follows the chain of calls that makes a statement, a.B().C(), and
// records the condition of each link that applies one: a helper, WHERE, the
// ON_CONFLICT columns, and the guard inside DO_UPDATE.
func (v *verifier) scanChain(e ast.Expr, f codeFacts) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch {
	case derivedHelpers[sel.Sel.Name] && len(call.Args) >= 2:
		v.coverConjuncts(call.Args[1], f.covered)
	case sel.Sel.Name == "WHERE" && len(call.Args) == 1:
		v.coverConjuncts(call.Args[0], f.covered)
	case sel.Sel.Name == "ON_CONFLICT":
		for _, arg := range call.Args {
			if field := tableField(arg); field != "" {
				f.covered[field] = true
			}
		}
	case sel.Sel.Name == "DO_UPDATE":
		for _, arg := range call.Args {
			v.scanChain(arg, f)
		}
	}
	v.scanChain(sel.X, f)
}

// coverConjuncts marks the scope fields that the conjuncts of cond cover.
func (v *verifier) coverConjuncts(cond ast.Expr, covered map[string]bool) {
	for _, c := range conjuncts(cond) {
		if field := v.coveredBy(c); field != "" {
			covered[field] = true
		}
	}
}

// conjuncts flattens a.AND(b).AND(c) into a, b, c.
func conjuncts(e ast.Expr) []ast.Expr {
	if call, ok := e.(*ast.CallExpr); ok {
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "AND" && len(call.Args) == 1 {
			return append(conjuncts(sel.X), call.Args[0])
		}
	}
	return []ast.Expr{e}
}

// coveredBy returns the field that the conjunct c constrains: r.where.F(x), a
// registered function of the context on the column (pkg.Filter(ctx, r.t.F),
// matched by import path and name against ContextScopes), or a guard
// r.t.F.EQ(r.t.EXCLUDED.F).
func (v *verifier) coveredBy(c ast.Expr) string {
	call, ok := c.(*ast.CallExpr)
	if !ok {
		return ""
	}
	if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
		if inner, isInner := sel.X.(*ast.SelectorExpr); isInner && inner.Sel.Name == "where" {
			return sel.Sel.Name
		}
		if (sel.Sel.Name == "EQ" || sel.Sel.Name == "IS_NOT_DISTINCT_FROM") && tableField(sel.X) != "" {
			return tableField(sel.X)
		}
	}
	fn := v.resolve(call.Fun)
	for _, arg := range call.Args {
		if field := tableField(arg); field != "" && fn != "" && fn == v.contextFunc(field) {
			return field
		}
	}
	return ""
}

// contextFunc is the function that ContextScopes registers for the column of
// the Go field, "" when it has none.
func (v *verifier) contextFunc(field string) string {
	for _, c := range v.r.b.table.columns {
		if c.field == field {
			return v.opts.ContextScopes[c.sqlName]
		}
	}
	return ""
}

// tableField returns F for the expression x.t.F, else "".
func tableField(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if inner, isInner := sel.X.(*ast.SelectorExpr); isInner && inner.Sel.Name == "t" {
			return sel.Sel.Name
		}
	}
	return ""
}

// resolve returns the import path and name of the function that fun calls:
// workspacescope.Present in a file that imports it from p is "p.Present". A
// name without a package is the package of the binding.
func (v *verifier) resolve(fun ast.Expr) string {
	switch fun := fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			if path, known := v.imports[id.Name]; known {
				return path + "." + fun.Sel.Name
			}
		}
	case *ast.Ident:
		return v.r.b.impl.Obj().Pkg().Path() + "." + fun.Name
	}
	return ""
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

// negatedCall returns the call in !f(...), else nil.
func negatedCall(cond ast.Expr) *ast.CallExpr {
	if un, isUn := cond.(*ast.UnaryExpr); isUn && un.Op == token.NOT {
		if call, isCall := un.X.(*ast.CallExpr); isCall {
			return call
		}
	}
	return nil
}

// method checks one derived method.
func (v *verifier) method(m *derivedMethod, fd *ast.FuncDecl) []string {
	f := v.scan(fd)
	var out []string
	if m.op.isWrite() {
		out = v.checkWrite(m, f)
	} else {
		out = v.checkRead(m, fd, f)
	}
	return append(out, v.checkPresent(m, f)...)
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

// checkPresent checks that a method whose context scopes must be found in ctx
// calls the registered Present function of each: a delete or write changing
// rows, and a read of a table whose only scopes are context scopes (a bare
// context would read every tenant), unless the name is Internal or binds the
// column from a parameter. The call is matched by import path and name.
func (v *verifier) checkPresent(m *derivedMethod, f codeFacts) []string {
	read := !m.op.changesRows()
	if read && !v.ctxOnly() {
		return nil
	}
	var out []string
	for _, c := range v.r.b.table.columns {
		fn := v.opts.ContextPresents[c.sqlName]
		if fn == "" || v.opts.ContextScopes[c.sqlName] == "" || !slices.Contains(v.opts.Scopes, c.sqlName) {
			continue
		}
		if read && (slices.Contains(m.cross, c.sqlName) || slices.Contains(m.skipped, c.field)) {
			continue
		}
		if !f.hasPresent[fn] {
			out = append(out, "does not check "+fn+" first: a context without "+c.sqlName+" reaches every value")
		}
	}
	return out
}

// ctxOnly reports whether every scope column of the table is a context scope.
func (v *verifier) ctxOnly() bool {
	found := false
	for _, c := range v.r.b.table.columns {
		if !slices.Contains(v.opts.Scopes, c.sqlName) {
			continue
		}
		if v.opts.ContextScopes[c.sqlName] == "" {
			return false
		}
		found = true
	}
	return found
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
