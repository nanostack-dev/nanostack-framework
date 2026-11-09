package genrepo

import (
	"go/token"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// searchAPI writes <Name>SearchFromAPI (design 4.2): the body properties
// that the request sends, onto a new builder, with the API defaults. A
// property that is not sent, or an empty list, filters nothing; no sort is
// the default sort; no limit is the default limit. own renders a name of
// the declaring package. A stub has a body that panics.
func (e *emitter) searchAPI(b *builder, own func(string) string, stub bool) {
	s, m := b.s, b.s.apiMapper
	q := s.name + "Search"
	params, args := e.mapperParams(b, own)
	e.p("// %s maps %s %s onto %s %s, with the API defaults.\n", mapperName(s), article(m.body.Obj().Name()),
		m.body.Obj().Name(), article(q), q)
	sig := "func " + mapperName(s) + "(" + strings.Join(append(params, "body *"+e.typ(m.body)), ", ") + ") " + own(q)
	if stub {
		e.p("%s { panic(\"nano gen stub\") }\n\n", sig)
		return
	}
	newQ := own("New"+q) + "(" + strings.Join(args, ", ") + ")"
	if m.filter == nil && m.fullText == nil && m.sort == "" && m.page == nil {
		e.p("%s { return %s }\n\n", sig, newQ)
		return
	}
	e.p("%s {\n\tq, b := %s, %s(body)\n", sig, newQ, e.sx("OrZero"))
	e.filterBinds(m)
	if f := m.fullText; f != nil {
		if f.ptr {
			e.p("\tq = %s(q, q.FullText, b.%s)\n", e.sx("Opt"), f.name)
		} else {
			e.p("\tq = q.FullText(b.%s)\n", f.name)
		}
	}
	dflt := ""
	if b.dflt != nil {
		dflt = own(s.name + "Sort" + b.dflt.name)
	}
	switch m.sort {
	case "APISort":
		dir := "(*string)(nil)" // the body has no sort_direction
		if m.sortDir != nil {
			dir = m.sortDir.arg()
		}
		e.p("\tq = %s(q, q.SortBy, %s, %s, %s, %s)\n", e.sx("APISort"), m.sortBy.arg(), dir, dflt,
			e.ref(searchPath, "search", m.dir))
	case "APIDirection":
		e.p("\tq = %s(q, q.SortBy, %s, %s)\n", e.sx("APIDirection"), dflt, m.sortDir.arg())
	}
	if m.page != nil {
		e.p("\treturn %s(q.Query, %s)\n}\n\n", e.sx("APIPage"), m.page.arg())
		return
	}
	e.p("\treturn q\n}\n\n")
}

// arg renders the field of the body b as the pointer that a searchexec
// helper takes.
func (f *apiField) arg() string {
	if f.ptr {
		return "b." + f.name
	}
	return "&b." + f.name
}

// filterBinds writes the binds of the filter object.
func (e *emitter) filterBinds(m *apiMapper) {
	if m.filter == nil {
		return
	}
	indent := "\t"
	if m.filter.ptr {
		e.p("\tif f := b.%s; f != nil {\n", m.filter.name)
		indent = "\t\t"
	} else {
		e.p("\tf := b.%s\n", m.filter.name)
	}
	for _, bind := range m.binds {
		set := "q." + bind.accessor + "()." + bind.method
		switch {
		case bind.list && bind.field.ptr:
			e.p("%sq = %s(q, %s, %s(f.%s))\n", indent, e.sx("Some"), set, e.sx("OrZero"), bind.field.name)
		case bind.list:
			e.p("%sq = %s(q, %s, f.%s)\n", indent, e.sx("Some"), set, bind.field.name)
		case bind.field.ptr:
			e.p("%sq = %s(q, %s, f.%s)\n", indent, e.sx("Opt"), set, bind.field.name)
		default:
			e.p("%sq = %s(f.%s)\n", indent, set, bind.field.name)
		}
	}
	if m.filter.ptr {
		e.p("\t}\n")
	}
}

// mapperParams returns the scope parameters of the mapper and the
// arguments of the constructor, as New<Name>Search takes them.
func (e *emitter) mapperParams(b *builder, own func(string) string) (params, args []string) {
	switch len(b.fields) {
	case 0:
		return nil, nil
	case 1:
		v := lowerFirst(b.fields[0].name)
		if token.IsKeyword(v) || v == "body" || v == "q" || v == "b" {
			v += "Value"
		}
		return []string{v + " " + e.typ(b.fields[0].typ)}, []string{v}
	}
	return []string{"scope " + own(b.scopeName())}, []string{"scope"}
}

// renderMappers renders the API mapper of each search whose handler is in
// another package, in that package (design S10). stub writes the bodies
// that panic, for the forward stubs.
func (l *loaded) renderMappers(searches []*searchDecl, stub bool) ([]gen.File, error) {
	var files []gen.File
	for _, s := range searches {
		m := s.apiMapper
		if m == nil || m.home == nil || s.builder == nil || len(s.unbuilt) > 0 {
			continue
		}
		e := &emitter{imp: importset.New(m.home.pkgPath), self: m.home.pkgPath}
		own := func(ident string) string { return e.ref(l.pkg.PkgPath, l.pkg.Name, ident) }
		e.searchAPI(s.builder, own, stub)
		src, err := e.file(m.home.pkgName, filepath.Base(m.home.file))
		if err != nil {
			return nil, err
		}
		files = append(files, gen.File{Path: m.home.file, Data: src})
	}
	return files, nil
}
