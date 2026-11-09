package genrepo

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// searchPath is the package of search.Result.
const searchPath = dbPath + "/search"

// searchMethod is an interface method that takes a declared search and
// that nanostack gen repo generates (design G8): it runs the search on the
// search hatch of the base, mapping each row with the mapper of Map or
// MapPage.
type searchMethod struct {
	fn     *types.Func
	s      *searchDecl
	result types.Type // search.Result[D]
	// mapper is how the method names the mapper: r.<field>.<Method>, a
	// function, or "" for the Row itself (no Map).
	mapper  string
	mapFunc types.Object // the function of a plain function mapper
	page    bool
}

// searchOf returns the declared search whose builder fn takes as its
// second parameter, after the context: q XSearch. The builder may not be
// generated yet, so an undefined parameter type is read by its name.
func (r *resolver) searchOf(d *declarations, fn *types.Func) *searchDecl {
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Params().Len() != 2 {
		return nil
	}
	name := ""
	if n, ok := types.Unalias(sig.Params().At(1).Type()).(*types.Named); ok && n.Obj().Pkg() == r.l.pkg.Types {
		name = n.Obj().Name()
	} else if id, isIdent := r.paramTypeExpr(fn, 1).(*ast.Ident); isIdent {
		name = id.Name
	}
	for _, s := range d.searches {
		if name != "" && s.name+"Search" == name {
			return s
		}
	}
	return nil
}

// paramTypeExpr returns the type expression of the i-th parameter of the
// interface method fn, as written.
func (r *resolver) paramTypeExpr(fn *types.Func, i int) ast.Expr {
	var out ast.Expr
	for _, f := range r.l.pkg.Syntax {
		ast.Inspect(f, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || out != nil || len(field.Names) != 1 || field.Names[0].Pos() != fn.Pos() {
				return out == nil
			}
			if ft, isFunc := field.Type.(*ast.FuncType); isFunc {
				out = nthParam(ft, i)
			}
			return false
		})
		if out != nil {
			break
		}
	}
	return out
}

// nthParam returns the type of the i-th parameter of ft, nil if it has
// fewer parameters.
func nthParam(ft *ast.FuncType, i int) ast.Expr {
	n := 0
	for _, p := range ft.Params.List {
		count := max(1, len(p.Names))
		if i < n+count {
			return p.Type
		}
		n += count
	}
	return nil
}

// resolveSearch generates fn on the search hatch of the base: E18 (a
// SortHandWritten sort it would refuse), E22 (the Map receiver) and E23
// (the result type). E21 is checked once every method is resolved.
func (r *resolver) resolveSearch(fn *types.Func, s *searchDecl) {
	q := s.name + "Search"
	for _, sort := range s.sorts {
		if sort.kind == "SortHandWritten" {
			r.fail(fn.Pos(), fn, fmt.Sprintf("write %s by hand on searchexec.Resolve, or declare the sort with "+
				"nanorepo.SortExpr", fn.Name()),
				"search %s: sort %s is hand-written, but %s is generated and would refuse it", s.name, sort.api[0],
				fn.Name())
			r.line(fn, "ERROR: generated search with a hand-written sort", "")
			return
		}
	}
	m := &searchMethod{fn: fn, s: s, page: s.mapper != nil && s.mapper.page}
	dom := s.row
	if s.mapper != nil {
		var ok bool
		if dom, ok = r.searchMapper(fn, s, m); !ok {
			return
		}
	}
	sig, _ := fn.Type().(*types.Signature)
	res := sig.Results()
	want := "(" + r.l.searchResult(dom, r.qualified) + ", error)"
	if res.Len() != 2 || !isSearchResult(res.At(0).Type(), dom) || !isError(res.At(1).Type()) {
		got := "nothing"
		if res.Len() > 0 {
			got = r.qualified(res.At(0).Type())
			if res.Len() > 1 {
				got = "(" + got + ", " + r.qualified(res.At(1).Type()) + ")"
			}
		}
		r.fail(fn.Pos(), fn, "change it to:"+codeIndent+fn.Name()+"(ctx context.Context, q "+q+") "+want,
			"%s returns %s; the search %s gives %s", fn.Name(), got, s.name, want)
		r.line(fn, "ERROR: the result is not the search result", "")
		return
	}
	m.result = res.At(0).Type()
	r.res.searches = append(r.res.searches, m)
	r.line(fn, fmt.Sprintf("search %s: generated, runs %s on the base's %s", s.name, specVar(s),
		map[bool]string{false: "search", true: "searchPage"}[m.page]), "")
}

// searchMapper reads the mapper of Map or MapPage: a function, or a method
// expression whose receiver type is the type of exactly one field of the
// implementation (E22). It returns the domain type D of a row.
func (r *resolver) searchMapper(fn *types.Func, s *searchDecl, m *searchMethod) (types.Type, bool) {
	info := r.l.pkg.TypesInfo
	e := ast.Unparen(s.mapper.fn)
	sig, _ := types.Unalias(info.TypeOf(e)).(*types.Signature)
	if sig == nil || sig.Results().Len() == 0 {
		return nil, false // the declaration check reports it
	}
	dom := sig.Results().At(0).Type()
	if m.page {
		if sl, ok := types.Unalias(dom).(*types.Slice); ok {
			dom = sl.Elem()
		}
	}
	sel, isSel := e.(*ast.SelectorExpr)
	if selection := info.Selections[sel]; isSel && selection != nil && selection.Kind() == types.MethodExpr {
		return dom, r.mapperField(fn, s, m, sel, selection.Recv())
	}
	obj, _ := info.Uses[tableIdent(e)].(*types.Func)
	if obj == nil {
		return nil, false
	}
	m.mapFunc = obj
	return dom, true
}

// mapperField sets the mapper of a method expression to the one field of
// the implementation of its receiver type (E22).
func (r *resolver) mapperField(fn *types.Func, s *searchDecl, m *searchMethod, sel *ast.SelectorExpr,
	recv types.Type,
) bool {
	var fields []string
	if st, ok := r.b.impl.Underlying().(*types.Struct); ok {
		for f := range st.Fields() {
			if types.Identical(f.Type(), recv) {
				fields = append(fields, f.Name())
			}
		}
	}
	if len(fields) != 1 {
		what := "no field"
		if len(fields) > 1 {
			what = "two fields (" + strings.Join(fields, ", ") + ")"
		}
		r.fail(s.mapper.fn.Pos(), fn, fmt.Sprintf("give %s one field of type %s, set in the constructor",
			r.b.impl.Obj().Name(), r.qualified(recv)),
			"Map(%s): %s has %s of type %s, so %s cannot call the mapper", types.ExprString(s.mapper.fn),
			r.b.impl.Obj().Name(), what, r.qualified(recv), fn.Name())
		r.line(fn, "ERROR: the Map receiver is not one field of the implementation", "")
		return false
	}
	m.mapper = "r." + fields[0] + "." + sel.Sel.Name
	return true
}

// searchResult renders search.Result[dom].
func (l *loaded) searchResult(dom types.Type, qualified func(types.Type) string) string {
	return "search.Result[" + qualified(dom) + "]"
}

// isSearchResult reports whether t is search.Result[dom] of db/search.
func isSearchResult(t types.Type, dom types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != searchPath || n.Obj().Name() != "Result" ||
		n.TypeArgs().Len() != 1 {
		return false
	}
	return types.Identical(n.TypeArgs().At(0), dom)
}

// checkHandSearch checks a hand-written method that takes a search (E19):
// it runs q.Query, unassigned, through searchexec.Resolve, Run or RunPage,
// transactor.Search or SearchPage, or the search hatches of the base, with
// its own ctx and the search's spec variable.
func (r *resolver) checkHandSearch(fn *types.Func, fd *ast.FuncDecl, s *searchDecl) {
	ctxName, qName := paramName(fd.Type, 0), paramName(fd.Type, 1)
	spec := specVar(s)
	file, line := r.l.position(r.l.pkg.Fset, fd.Pos())
	want := fmt.Sprintf("searchexec.Resolve(%s, %s, %s.Query)", named(ctxName, "ctx"), spec, named(qName, "q"))
	fail := func(why string) {
		r.fail(fd.Pos(), fn, "run it as "+want+" (or searchexec.Run, RunPage, or the base's search and "+
			"searchPage), with the ctx and q of the method",
			"the hand-written method (%s:%d) takes %s but %s: it skips the declared scope", file, line,
			article(s.name+"Search")+" "+s.name+"Search", why)
	}
	if fd.Body == nil || qName == "" || qName == "_" {
		fail("never runs q.Query through " + want)
		return
	}
	reassigned, runs := false, false
	var bad string
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == qName {
					reassigned = true
				}
			}
		case *ast.CallExpr:
			at := r.runsSearch(x) // the index of the spec argument
			if at < 0 || len(x.Args) < at+2 {
				return true
			}
			switch {
			case !isIdentNamed(x.Args[0], ctxName):
				bad = "runs it with " + types.ExprString(x.Args[0]) + ", not the ctx of the method"
			case !isIdentNamed(x.Args[at], spec):
				bad = "runs it with the spec " + types.ExprString(x.Args[at]) + ", not " + spec
			case !isQuery(x.Args[at+1], qName):
				bad = "runs " + types.ExprString(x.Args[at+1]) + ", not " + qName + ".Query"
			default:
				runs = true
			}
		}
		return true
	})
	switch {
	case reassigned:
		fail("assigns " + qName + ", so the query it runs is not the one the caller built")
	case runs:
	case bad != "":
		fail(bad)
	default:
		fail("never runs " + qName + ".Query through " + want)
	}
}

// runsSearch returns the index of the spec argument when call runs a
// search, else -1: searchexec.Resolve (1), Run or RunPage (2),
// transactor.Search or SearchPage (2), or a method search or searchPage (1:
// the base hatches, which the first load does not have yet).
func (r *resolver) runsSearch(call *ast.CallExpr) int {
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return -1
	}
	if obj, isFunc := r.l.pkg.TypesInfo.Uses[sel.Sel].(*types.Func); isFunc && obj.Pkg() != nil {
		switch {
		case obj.Pkg().Path() == searchexecPath && obj.Name() == "Resolve":
			return 1
		case obj.Pkg().Path() == searchexecPath && slices.Contains([]string{"Run", "RunPage"}, obj.Name()),
			obj.Pkg().Path() == transactorPath && slices.Contains([]string{"Search", "SearchPage"}, obj.Name()):
			return 2
		}
	}
	if sel.Sel.Name == "search" || sel.Sel.Name == "searchPage" {
		return 1
	}
	return -1
}

// named returns name, or dflt for a blank or missing name.
func named(name, dflt string) string {
	if name == "" || name == "_" {
		return dflt
	}
	return name
}

// paramName returns the name of the i-th parameter of ft, "" if unnamed.
func paramName(ft *ast.FuncType, i int) string {
	n := 0
	for _, p := range ft.Params.List {
		for _, name := range p.Names {
			if n == i {
				return name.Name
			}
			n++
		}
		if len(p.Names) == 0 {
			n++
		}
	}
	return ""
}

func isIdentNamed(e ast.Expr, name string) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == name
}

// isQuery reports whether e is q.Query.
func isQuery(e ast.Expr, q string) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Query" && isIdentNamed(sel.X, q)
}

// unusedSearches reports each declared search that no method of a bound
// interface takes, and no hand-written method takes (E24). A package that
// binds no interface runs its searches in hand-written code only, so the
// rule applies once the package binds one.
func (l *loaded) unusedSearches(d *declarations, res []*resolution) []error {
	if len(d.bindings) == 0 || len(res) == 0 {
		return nil
	}
	taken := l.takenTypes(res)
	var out []error
	iface := res[0].explain.Iface
	for _, s := range d.searches {
		q := s.name + "Search"
		if taken[q] || s.builder == nil || len(s.unbuilt) > 0 {
			continue
		}
		dom := l.searchDomain(s)
		out = append(out, gen.Errorf(s.at.file, s.at.line,
			fmt.Sprintf("add Search%s(ctx context.Context, q %s) (search.Result[%s], error) to %s, or remove "+
				"the declaration", pluralName(s.name), q, dom, iface),
			"search %s is declared, and no method takes %s %s", s.name, article(q), q))
	}
	return out
}

// takenTypes are the parameter type names of the methods of the bound
// interfaces and of the hand-written methods of the package.
func (l *loaded) takenTypes(res []*resolution) map[string]bool {
	taken := map[string]bool{}
	for _, r := range res {
		for _, name := range r.takes {
			taken[name] = true
		}
	}
	for _, f := range l.pkg.Syntax {
		if l.isOwn(l.pkg.Fset.Position(f.Pos()).Filename) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			for i := range fd.Type.Params.NumFields() {
				if id, isIdent := nthParam(fd.Type, i).(*ast.Ident); isIdent {
					taken[id.Name] = true
				}
			}
		}
	}
	return taken
}

// searchDomain renders the domain type of a search's rows: the output of
// its mapper, or its Row.
func (l *loaded) searchDomain(s *searchDecl) string {
	if s.mapper == nil {
		return types.TypeString(s.row, l.qualifier())
	}
	sig, ok := types.Unalias(l.pkg.TypesInfo.TypeOf(s.mapper.fn)).(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return "<Domain>"
	}
	t := sig.Results().At(0).Type()
	if sl, isSlice := types.Unalias(t).(*types.Slice); isSlice && s.mapper.page {
		t = sl.Elem()
	}
	return types.TypeString(t, l.qualifier())
}

// qualifier names the types of other packages by their package name.
func (l *loaded) qualifier() types.Qualifier {
	return func(p *types.Package) string {
		if p == l.pkg.Types {
			return ""
		}
		return p.Name()
	}
}

// pluralName is the plural of a search name, for a method name: Webhook
// gives Webhooks.
func pluralName(name string) string {
	switch {
	case strings.HasSuffix(name, "s"), strings.HasSuffix(name, "x"):
		return name + "es"
	case strings.HasSuffix(name, "y") && !strings.HasSuffix(name, "ey"):
		return strings.TrimSuffix(name, "y") + "ies"
	}
	return name + "s"
}
