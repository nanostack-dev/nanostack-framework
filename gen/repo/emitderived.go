package genrepo

import (
	"go/format"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// derivedSuffix ends the name of the file nano gen repo writes next to a
// declaring file: product_user_repository.go gets
// product_user_repository_repo.gen.go.
const derivedSuffix = "_repo.gen.go"

// derivedFile returns the file that holds the derived methods of the
// bindings declared in file, a path relative to the package.
func derivedFile(file string) string {
	return strings.TrimSuffix(filepath.Base(file), ".go") + derivedSuffix
}

// renderDerived returns one <file>_repo.gen.go per declaring file with a
// binding or a search: the var _ line and the derived methods of each
// binding (design D7, D14), then the builder of each search (design 4.2).
// Each file maps to the bindings and the searches it holds, in file order.
func (l *loaded) renderDerived(res []*resolution, searches []*searchDecl) ([]gen.File, *rendered, error) {
	out := &rendered{byFile: map[string][]*resolution{}, searches: map[string][]*searchDecl{}}
	var names []string
	add := func(name string) {
		if _, seen := out.byFile[name]; !seen && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for _, r := range res {
		name := derivedFile(r.b.file)
		add(name)
		out.byFile[name] = append(out.byFile[name], r)
	}
	for _, s := range searches {
		name := derivedFile(s.at.file)
		add(name)
		out.searches[name] = append(out.searches[name], s)
	}
	slices.Sort(names)
	files := make([]gen.File, 0, len(names))
	for _, name := range names {
		e := &emitter{imp: importset.New(l.pkg.PkgPath), self: l.pkg.PkgPath}
		e.imp.Block(l.pkg.Types.Scope().Names()...)
		for _, r := range out.byFile[name] {
			e.binding(r)
		}
		for _, s := range out.searches[name] {
			if s.builder != nil && len(s.unbuilt) == 0 {
				e.searchBuilder(s.builder)
				if s.apiMapper != nil && s.apiMapper.home == nil {
					e.searchAPI(s.builder, func(ident string) string { return ident }, false)
				}
			}
		}
		src, err := e.file(l.pkg.Name, name)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, gen.File{Path: filepath.Join(l.dir, name), Data: src})
	}
	return files, out, nil
}

// file renders the header, the package clause, the imports and the code of
// e, formatted.
func (e *emitter) file(pkg, name string) ([]byte, error) {
	var out strings.Builder
	out.WriteString(header)
	out.WriteString("package " + pkg + "\n\n")
	e.imp.Write(&out)
	out.WriteString(e.b.String())
	src, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, gen.Errorf("", 0, "report this nano gen repo bug with the declaration that caused it",
			"the generated %s does not parse: %v", name, err)
	}
	return src, nil
}

// binding writes the var _ line of one binding and its derived methods.
func (e *emitter) binding(r *resolution) {
	b, t := r.b, r.b.table
	iface, impl := e.typ(b.iface), b.impl.Obj().Name()
	methods := "derived methods"
	if len(r.derived) == 1 {
		methods = "derived method"
	}
	e.p("// %s on *%s: %d %s on table %s; the other methods are\n", iface, impl, len(r.derived), methods, t.sqlName)
	e.p("// hand-written (nano gen repo -explain lists each one).\n")
	if len(r.overrides) > 0 {
		e.p("//\n// Hand-written although the name derives (deleting the method switches it\n")
		e.p("// to the derived query): %s.\n", strings.Join(r.overrides, ", "))
	}
	e.p("//\n// A compile error on the next line means %s gained or lost a method since\n", iface)
	e.p("// the last run: run nano gen repo.\n")
	e.p("var _ %s = (*%s)(nil)\n\n", iface, impl)

	recv := "r " + impl
	if r.recvPtr {
		recv = "r *" + impl
	}
	rs := &resolver{t: t}
	for _, m := range r.derived {
		e.p("// %s is derived from its name:\n//\n//\t%s\n", m.fn.Name(), rs.methodSQL(m))
		if m.unscoped {
			cols := make([]string, len(m.skipped))
			for i, f := range m.skipped {
				cols[i] = columnOf(t, f)
			}
			e.p("//\n// UNSCOPED: the name ends in Internal, so it does not filter on %s.\n",
				strings.Join(cols, " or "))
		}
		e.p("func (%s) %s(%s) %s {\n%s}\n\n", recv, m.fn.Name(), e.params(m), e.results(m, t), e.body(m, r.via))
	}
	for _, m := range r.searches {
		e.searchMethod(m, recv, impl, r.via)
	}
}

// searchMethod writes a generated method that runs a declared search on
// the search hatch of the base (design G8).
func (e *emitter) searchMethod(m *searchMethod, recv, impl, via string) {
	s, q := m.s, m.s.name+"Search"
	e.p("// %s runs %s %s: *%s has no hand-written %s.\n", m.fn.Name(), article(q), q, impl, m.fn.Name())
	ctx := e.ref("context", "context", "Context")
	row := e.typ(s.row)
	mapper := m.mapper
	switch {
	case m.mapFunc != nil:
		mapper = e.fn(m.mapFunc)
	case mapper == "":
		mapper = "func(row " + row + ") " + row + " { return row }"
	}
	hatch := "search"
	if m.page {
		hatch = "searchPage"
	}
	e.p("func (%s) %s(ctx %s, q %s) (%s, error) {\n", recv, m.fn.Name(), ctx, q, e.typ(m.result))
	e.p("\treturn %s%s(ctx, %s, q.Query, %s)\n}\n\n", via, hatch, specVar(s), mapper)
}

// params renders the parameter list of a derived method.
func (e *emitter) params(m *derivedMethod) string {
	parts := []string{"ctx " + e.ref("context", "context", "Context")}
	for _, p := range m.params {
		parts = append(parts, p.name+" "+e.typ(p.typ))
	}
	return strings.Join(parts, ", ")
}

// results renders the result list of a derived method. Only a method that
// returns rows names the domain type, so only it imports the type's package:
// a derived file whose methods return no row must not import it.
func (e *emitter) results(m *derivedMethod, t *tableBase) string {
	switch m.shape {
	case shapeOption:
		return "(" + e.ref(functionalPath, "functional", "Option") + "[" + e.typ(t.domain) + "], error)"
	case shapeSlice:
		return "([]" + e.typ(t.domain) + ", error)"
	case shapeOne:
		return "(" + e.typ(t.domain) + ", error)"
	case shapeBool, shapeDeleted:
		return "(bool, error)"
	case shapeInt64, shapeAffected:
		return "(int64, error)"
	case shapeErr:
	}
	return "error"
}

// body renders the one helper call of a derived method on the base.
func (e *emitter) body(m *derivedMethod, via string) string {
	parts := make([]string, len(m.preds))
	for i, pred := range m.preds {
		arg := m.params[i].name
		if m.params[i].conv != nil {
			arg = e.typ(m.params[i].conv) + "(" + arg + ")"
		}
		parts[i] = via + "where." + pred + "(" + arg + ")"
	}
	cond := andAll(parts)
	call := func(helper string) string { return via + helper + "(ctx, " + cond + ")" }
	switch m.shape {
	case shapeOption:
		return "\treturn " + call("findOne") + "\n"
	case shapeSlice:
		return "\treturn " + call("findAll") + "\n"
	case shapeOne:
		return "\tfound, err := " + call("findOne") + "\n\trow, ok := found.Get()\n" +
			"\tif err == nil && !ok {\n\t\terr = " + e.ref("database/sql", "sql", "ErrNoRows") + "\n\t}\n" +
			"\treturn row, err\n"
	case shapeBool:
		return "\treturn " + call("exists") + "\n"
	case shapeInt64:
		return "\treturn " + call("count") + "\n"
	case shapeErr:
		return "\t_, err := " + call("deleteWhere") + "\n\treturn err\n"
	case shapeDeleted:
		return "\tn, err := " + call("deleteWhere") + "\n\treturn n > 0, err\n"
	case shapeAffected:
	}
	return "\treturn " + call("deleteWhere") + "\n"
}

// bindingAtLine returns the binding of a derived file whose code holds
// line: the last var _ line at or before it, else the first binding.
func bindingAtLine(src []byte, line int, res []*resolution, typ func(*resolution) string) *resolution {
	at := res[0]
	for i, text := range strings.Split(string(src), "\n") {
		if i >= line {
			break
		}
		for _, r := range res {
			if strings.HasPrefix(text, "var _ "+typ(r)+" = (*"+r.b.impl.Obj().Name()+")(nil)") {
				at = r
			}
		}
	}
	return at
}
