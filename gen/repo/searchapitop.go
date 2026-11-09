package genrepo

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/db/search"
)

// This file holds the top-level properties of the API binding (design
// S13): full_text_search, sort_by and sort_direction, pagination, and the
// properties that nothing reads (A10); and the value types and limits of
// a bound property.

// apiOption reads API, IgnoresFullText and APIHandWritten.
func (c *collector) apiOption(s *searchDecl, name string, call *ast.CallExpr) {
	switch name {
	case "API":
		if inst, ok := c.l.pkg.TypesInfo.Instances[c.funcIdent(call)]; ok && inst.TypeArgs.Len() == 1 {
			s.api, s.apiNode = inst.TypeArgs.At(0), call
		}
	case "IgnoresFullText":
		s.ignoresFullText, _ = c.reason(s, name, call.Args[0])
		s.ignoresNode = call
	case "APIHandWritten":
		if s.handNode == nil {
			s.handNode = call
		}
		for _, a := range call.Args {
			if prop, ok := c.constString(s, a); ok {
				s.apiHandWritten = append(s.apiHandWritten, prop)
			}
		}
	}
}

// fullText checks full_text_search against FullText and IgnoresFullText
// (A5, A6).
func (a *apiCheck) fullText(p *apiProp) {
	s := a.s
	bound := false // a FullText entry that the API reaches
	for _, col := range s.fullText {
		bound = bound || !s.fullTextGoOnly[col.expr]
	}
	for _, fn := range s.fullTextExprs {
		bound = bound || !s.fullTextGoOnly[fn]
	}
	switch {
	case p != nil && bound && s.ignoresNode != nil:
		a.errorf(s.ignoresNode, "remove IgnoresFullText: FullText reads full_text_search",
			"search %s: FullText reads full_text_search, and IgnoresFullText ignores it", s.name)
	case p != nil && bound:
		a.read[propFullText] = true
		a.bindFullText(p)
	case p != nil && s.ignoresNode != nil && p.own:
		a.read[propFullText] = true // the problem is this one, not A10's
		a.errorf(s.ignoresNode, "declare nanorepo.FullText(<columns>): only an inherited full_text_search may be "+
			"ignored", "search %s: %s declares full_text_search in its own schema, and IgnoresFullText ignores it: "+
			"the endpoint offers a term that it never reads", s.name, a.name)
	case p != nil && s.ignoresNode != nil:
		a.read[propFullText] = true
	case p != nil: // A5, not A10 too
		a.read[propFullText] = true
		a.errorf(s.apiNode, "nanorepo.FullText(<columns>), or nanorepo.IgnoresFullText(\"<why>\") while the "+
			"product decides", "%s accepts full_text_search but search %s has no FullText: the term would be ignored",
			a.name, s.name)
	case bound: // A6
		a.errorf(s.apiNode, "add full_text_search to "+a.name+", or wrap FullText in nanorepo.GoOnly(...)",
			"search %s has FullText, and %s has no full_text_search: API clients cannot use it", s.name, a.name)
	case s.ignoresNode != nil:
		a.errorf(s.ignoresNode, "remove IgnoresFullText", "search %s: IgnoresFullText, and %s has no "+
			"full_text_search: it exempts nothing", s.name, a.name)
	}
}

// bindFullText binds full_text_search to FullText.
func (a *apiCheck) bindFullText(p *apiProp) {
	v, ok := a.field(a.bodyStruct(), a.name, p)
	if !ok {
		return
	}
	f, ok := a.scalar(v, types.Typ[types.String], a.name+"."+p.name, p.line)
	if !ok {
		return
	}
	a.termLength(a.name+"."+p.name, p.schema)
	a.m.fullText = f
}

// scalar checks a top-level body field of the value type want, as a
// pointer or a value. ok is false after a problem.
func (a *apiCheck) scalar(v *types.Var, want types.Type, where string, line int) (*apiField, bool) {
	t := types.Unalias(v.Type())
	f := &apiField{name: v.Name()}
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		f.ptr, t = true, ptr.Elem()
	}
	if !types.Identical(t, want) {
		a.specErrorf(line, "give "+where+" the type "+a.c.qualified(want),
			"%s is %s, and the mapper passes a %s", where, a.c.qualified(v.Type()), a.c.qualified(want))
		return nil, false
	}
	return f, true
}

// sorts checks sort_by against the sorts (A1, A2), and plans APISort or
// APIDirection.
func (a *apiCheck) sorts(by, dir *apiProp) {
	s := a.s
	if by == nil && dir == nil {
		return
	}
	a.read[propSortBy], a.read[propSortDir] = true, true // the problems below are not A10's
	if a.b.dflt == nil {
		a.errorf(s.apiNode, "add nanorepo.DefaultSort(column, direction): a sort_direction sent alone applies to it",
			"search %s: %s has sort_direction, and the search has no DefaultSort", s.name, a.name)
		return
	}
	if dir != nil {
		a.m.sortDir = a.direction(dir, propSortDir)
	}
	if by == nil {
		a.m.sort = "APIDirection"
		return
	}
	a.m.sort = "APISort"
	a.sortValues(by)
	a.m.sortBy = a.direction(by, propSortBy)
	switch a.c.l.sortByDirection {
	case "ASC":
		a.m.dir = "SortAscending"
	case "DESC":
		a.m.dir = "SortDescending"
	default:
		a.errorf(s.apiNode, `set API.SortByDirection in nanogen/project.go to "ASC" or "DESC", as the spec `+
			"describes a sort_by sent without a sort_direction",
			"search %s: %s has sort_by, and nanogen/project.go gives no direction for a sort_by sent alone",
			s.name, a.name)
	}
}

// sortValues compares the sort_by enum with the API names of the sorts.
func (a *apiCheck) sortValues(by *apiProp) {
	s := a.s
	var declared []string
	for _, sort := range s.sorts {
		declared = append(declared, sort.api...)
	}
	enum, line, ok := a.spec.enum(by.schema)
	if !ok {
		a.specErrorf(by.line, "give "+a.name+".sort_by an enum of the sorts: "+strings.Join(declared, ", "),
			"%s.sort_by has no enum: the sorts of search %s cannot be checked", a.name, s.name)
		return
	}
	for _, v := range enum { // A1
		if !slices.Contains(declared, v) {
			a.specErrorf(line, "add a sort with the API name "+v+" to search "+s.name+", or remove "+v+" from the enum",
				"%s.sort_by: value %q has no sort in search %s", a.name, v, s.name)
		}
	}
	for _, name := range declared { // A2
		if !slices.Contains(enum, name) {
			a.errorf(s.apiNode, "add "+name+" to the sort_by enum of "+a.name+", or remove the sort",
				"sort %s is not a value of %s.sort_by: API clients cannot ask for it", name, a.name)
		}
	}
}

// direction checks sort_by or sort_direction: a string enum, as a pointer,
// or a value the spec requires.
func (a *apiCheck) direction(p *apiProp, name string) *apiField {
	v, ok := a.field(a.bodyStruct(), a.name, p)
	if !ok {
		return nil
	}
	where := a.name + "." + name
	t := types.Unalias(v.Type())
	f := &apiField{name: v.Name()}
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		f.ptr, t = true, ptr.Elem()
	}
	if basic, isBasic := t.Underlying().(*types.Basic); !isBasic || basic.Info()&types.IsString == 0 {
		a.specErrorf(p.line, "give "+where+" a string type", "%s is %s, not a string", where,
			a.c.qualified(v.Type()))
		return nil
	}
	if !f.ptr && !p.required {
		a.specErrorf(p.line, "give "+where+" a pointer type (remove x-go-type-skip-optional-pointer)",
			"%s is an optional %s, and the mapper cannot tell a property that the client did not send",
			where, a.c.qualified(v.Type()))
		return nil
	}
	return f
}

// pagination checks the page object: the db/search type, the default
// limit of both sides, and the maximum (A9).
func (a *apiCheck) pagination(p *apiProp) {
	if p == nil {
		return
	}
	a.read[propPage] = true
	v, ok := a.field(a.bodyStruct(), a.name, p)
	if !ok {
		return
	}
	where := a.name + "." + propPage
	if !isNamed(deref(v.Type()), searchPath, "Pagination") {
		a.specErrorf(p.line, "give "+where+" the x-go-type search.Pagination, with the x-go-type-import "+searchPath,
			"%s is %s, and searchexec.APIPage reads a db/search Pagination", where, a.c.qualified(v.Type()))
		return
	}
	_, isPtr := types.Unalias(v.Type()).(*types.Pointer)
	a.m.page = &apiField{name: v.Name(), ptr: isPtr}
	limit := prop(a.props(p.schema), "limit")
	if limit == nil {
		return
	}
	want := a.s.defaultLimit
	if want == 0 {
		want = defaultLimit
	}
	if def, line, isSet := a.spec.number(limit.schema, "default"); isSet && def != int64(want) {
		a.specErrorf(line, "make the two defaults equal: nanorepo.DefaultLimit(n) in the search, or the default "+
			"of "+where+".limit", "%s.limit: the spec default is %d, the search default is %d", where, def, want)
	}
	if maxLimit, line, isSet := a.spec.number(limit.schema, "maximum"); isSet && maxLimit > search.MaxLimit {
		a.specErrorf(line, "set the maximum of "+where+".limit to 1000 (search.MaxLimit) or less",
			"%s.limit: maximum %d is above search.MaxLimit (%d)", where, maxLimit, search.MaxLimit)
	}
}

// unread reports a body property that the mapper does not read and that
// no exemption names (A10), and an APIHandWritten property that is not in
// the body or that the mapper reads itself.
func (a *apiCheck) unread(props []apiProp) {
	s := a.s
	filterProps := map[string]bool{}
	if fp := prop(props, propFilter); fp != nil {
		ps, _ := a.spec.props(fp.schema)
		for _, p := range ps {
			filterProps[p.name] = true
		}
	}
	for _, name := range s.apiHandWritten {
		switch {
		case name == propFilter || name == propFullText || name == propSortBy || name == propSortDir ||
			name == propPage:
			fix := "remove " + name + " from APIHandWritten: the generated mapper reads it"
			if name == propFullText {
				fix = "use nanorepo.IgnoresFullText(\"<why>\") for an inherited full_text_search"
			}
			a.errorf(s.handNode, fix, "search %s: APIHandWritten names %s, which the mapper reads", s.name, name)
		case prop(props, name) == nil && !filterProps[name]:
			a.errorf(s.handNode, "remove "+name+" from APIHandWritten", "search %s: APIHandWritten names %q, "+
				"which %s does not have: it exempts nothing", s.name, name, a.name)
		case prop(props, name) != nil:
			a.read[name] = true
		}
	}
	for _, p := range props {
		if a.read[p.name] {
			continue
		}
		fix := "bind it in search " + s.name + ", or name it with nanorepo.APIHandWritten(\"" + p.name + "\")"
		if p.name == propFilter {
			fix = "give the filter object a property that a filter binds, or remove it"
		}
		a.specErrorf(p.line, fix, "%s.%s is accepted and never read", a.name, p.name)
	}
}

// timeType returns time.Time from the imports of the declaring package,
// or nil when it does not import the time package.
func (l *loaded) timeType() types.Type {
	pkg, err := l.typesOf("time")
	if err != nil || pkg == nil {
		return nil
	}
	if obj := pkg.Scope().Lookup("Time"); obj != nil {
		return obj.Type()
	}
	return nil
}

// wantType returns the value type that method takes, nil when unknown.
func (a *apiCheck) wantType(bf *builderField, method string) types.Type {
	switch method {
	case "Contains", "ContainsAny":
		return types.Typ[types.String]
	case "Present", "Is":
		return types.Typ[types.Bool]
	case "Since", "Until", "After", "Before":
		if obj := a.c.l.timeType(); obj != nil {
			return obj
		}
		return nil
	}
	return bf.value
}

// limits reports a property whose spec allows more than the runtime
// accepts (A9): the runtime would refuse what the spec promises.
func (a *apiCheck) limits(where string, p *apiProp, method string, list bool) {
	if n, line, ok := a.spec.number(p.schema, "maxItems"); ok && list && n > search.MaxInValues {
		a.specErrorf(line, "set maxItems of "+where+" to 1000 (search.MaxInValues) or less",
			"%s: maxItems %d is above search.MaxInValues (%d)", where, n, search.MaxInValues)
	}
	term := p.schema
	if method == "ContainsAny" {
		term = a.spec.items(p.schema)
	} else if method != "Contains" {
		return
	}
	a.termLength(where, term)
}

// termLength reports a text term whose maxLength is above the runtime's.
func (a *apiCheck) termLength(where string, schema *yaml.Node) {
	if n, line, ok := a.spec.number(schema, "maxLength"); ok && n > search.MaxTermLength {
		a.specErrorf(line, "set maxLength of "+where+" to 1000 (search.MaxTermLength) or less",
			"%s: maxLength %d is above search.MaxTermLength (%d)", where, n, search.MaxTermLength)
	}
}
