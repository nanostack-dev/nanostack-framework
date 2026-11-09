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
// binding: its derived methods and the var _ line of each binding (design
// D7, D14). Each file maps to the bindings it holds, in file order.
func (l *loaded) renderDerived(res []*resolution) ([]gen.File, map[string][]*resolution, error) {
	byFile := map[string][]*resolution{}
	var names []string
	for _, r := range res {
		name := derivedFile(r.b.file)
		if _, seen := byFile[name]; !seen {
			names = append(names, name)
		}
		byFile[name] = append(byFile[name], r)
	}
	slices.Sort(names)
	files := make([]gen.File, 0, len(names))
	for _, name := range names {
		e := &emitter{imp: importset.New(l.pkg.PkgPath), self: l.pkg.PkgPath}
		e.imp.Block(l.pkg.Types.Scope().Names()...)
		for _, r := range byFile[name] {
			e.binding(r)
		}
		var out strings.Builder
		out.WriteString(header)
		out.WriteString("package " + l.pkg.Name + "\n\n")
		e.imp.Write(&out)
		out.WriteString(e.b.String())
		src, err := format.Source([]byte(out.String()))
		if err != nil {
			return nil, nil, gen.Errorf("", 0, "report this nano gen repo bug with the declaration that caused it",
				"the generated %s does not parse: %v", name, err)
		}
		files = append(files, gen.File{Path: filepath.Join(l.dir, name), Data: src})
	}
	return files, byFile, nil
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
