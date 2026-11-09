package genrepo

import (
	"go/ast"
	"go/types"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// The body properties of the shared search request that the mapper reads
// itself. APIHandWritten cannot name them.
const (
	propFilter   = "filter"
	propFullText = "full_text_search"
	propSortBy   = "sort_by"
	propSortDir  = "sort_direction"
	propPage     = "pagination"
)

// apiMapper is <Name>SearchFromAPI, the mapper from the request body of
// the API to the builder (design S13). nanostack gen repo writes it only when
// the body and the declaration agree both ways.
type apiMapper struct {
	body     *types.Named
	filter   *apiField // the filter object, nil when the mapper reads no filter property
	binds    []apiBind // in declaration order
	fullText *apiField // full_text_search, nil when the mapper does not read it
	sort     string    // APISort, APIDirection or ""
	sortBy   *apiField
	sortDir  *apiField // nil with APISort: the body has no sort_direction
	dir      string    // the db/search constant of the direction of a sort_by sent alone
	page     *apiField
	home     *apiHome // nil: the mapper goes in the declaring file
}

// apiField is a field of the body, or of its filter object.
type apiField struct {
	name  string // the Go field
	ptr   bool   // a pointer: nil is a property that was not sent
	slice bool   // a slice, or a pointer to a slice
}

// apiBind binds one filter property to a method of a builder field.
type apiBind struct {
	accessor string // the accessor of the builder, such as Status
	method   string // In, Eq, Since...
	list     bool   // the method takes a list
	field    apiField
}

// apiCheck is the both-ways check of one search: the body against the
// declaration (design section 5, A1 to A10).
type apiCheck struct {
	c        *collector
	s        *searchDecl
	b        *builder
	spec     *apiSpec
	body     *types.Named
	name     string // the body schema, such as WebhookRequestSearchRequest
	m        *apiMapper
	hand     map[string]bool // the properties that APIHandWritten names
	read     map[string]bool // the body properties that the mapper reads, or an exemption names
	problems int
}

// planAPI checks the API binding of a search whose builder nanostack gen repo
// writes, and plans its mapper. It reports an exemption without API too.
func (c *collector) planAPI(s *searchDecl) {
	if s.api == nil {
		if s.ignoresNode != nil {
			c.errorf(s.ignoresNode, "remove IgnoresFullText, or declare the request body with nanorepo.API[Body]()",
				"search %s: IgnoresFullText exempts a full_text_search property, and the search declares no API",
				s.name)
		}
		if s.handNode != nil {
			c.errorf(s.handNode, "remove APIHandWritten, or declare the request body with nanorepo.API[Body]()",
				"search %s: APIHandWritten names body properties, and the search declares no API", s.name)
		}
		return
	}
	body, ok := types.Unalias(s.api).(*types.Named)
	if _, isStruct := s.api.Underlying().(*types.Struct); !ok || !isStruct {
		c.errorf(
			s.apiNode,
			"pass the request body struct that nanostack gen api writes, such as API[apigen.XSearchRequest]",
			"search %s: API[%s] is not a named struct",
			s.name,
			c.qualified(s.api),
		)
		return
	}
	if s.builder == nil || len(s.unbuilt) > 0 {
		return // the problem of the builder is reported; the mapper needs the builder
	}
	if c.l.stub {
		home, _ := c.l.apiHome(s, body)
		s.apiMapper = &apiMapper{body: body, home: home}
		return
	}
	if c.l.specPath == "" { // A7
		c.errorf(s.apiNode, "set API.Spec in nanogen/project.go, or run nanostack gen repo with -spec <openapi file>",
			"search %s declares API[%s], and nanogen/project.go has no API: the sort_by values and the page "+
				"size cannot be checked", s.name, body.Obj().Name())
		return
	}
	spec, first, err := c.l.readSpec()
	if err != nil {
		if first {
			c.problems = append(c.problems, err)
		}
		return
	}
	a := &apiCheck{c: c, s: s, b: s.builder, spec: spec, body: body, name: body.Obj().Name(),
		m: &apiMapper{body: body}, hand: map[string]bool{}, read: map[string]bool{}}
	if a.check() {
		home, homeErr := c.l.apiHome(s, body)
		if homeErr != nil {
			c.problems = append(c.problems, homeErr)
			return
		}
		a.m.home = home
		s.apiMapper = a.m
	}
}

// errorf reports a problem at a node of the declaration.
func (a *apiCheck) errorf(n ast.Node, fix, format string, args ...any) {
	a.problems++
	a.c.errorf(n, fix, format, args...)
}

// specErrorf reports a problem at a line of the spec.
func (a *apiCheck) specErrorf(line int, fix, format string, args ...any) {
	a.problems++
	a.c.problems = append(a.c.problems, gen.Errorf(a.spec.rel, line, fix, format, args...))
}

// check runs A1 to A10 and fills the mapper. It reports whether the
// binding has no problem.
func (a *apiCheck) check() bool {
	schema := a.spec.schemas[a.name]
	if schema == nil {
		a.errorf(a.s.apiNode, "name the request body schema of the spec, or add schema "+a.name+" to it",
			"search %s: the OpenAPI spec has no schema %s", a.s.name, a.name)
		return false
	}
	if len(a.spec.bodies[a.name]) == 0 { // A8
		a.errorf(a.s.apiNode, "reference "+a.name+" from the JSON requestBody of the search operation",
			"no operation of the OpenAPI spec takes %s as its request body", a.name)
	}
	props := a.props(schema)
	for _, p := range a.s.apiHandWritten {
		a.hand[p] = true
	}
	a.filters(prop(props, propFilter))
	a.fullText(prop(props, propFullText))
	a.sorts(prop(props, propSortBy), prop(props, propSortDir))
	a.pagination(prop(props, propPage))
	a.unread(props) // A10
	return a.problems == 0
}

// props returns the properties of a schema, and reports each reference
// that the check cannot follow.
func (a *apiCheck) props(n *yaml.Node) []apiProp {
	props, unresolved := a.spec.props(n)
	for _, ref := range unresolved {
		a.specErrorf(ref.Line, "reference a schema of components.schemas in the same document",
			"%s: nanostack gen repo cannot follow $ref %s, so it cannot check the properties behind it", a.name,
			ref.Value)
	}
	return props
}

// field returns the Go field of the body struct st for property p, and
// reports a missing field.
func (a *apiCheck) field(st *types.Struct, owner string, p *apiProp) (*types.Var, bool) {
	if v := fieldByJSON(st, p.name); v != nil {
		return v, true
	}
	a.specErrorf(p.line, "run nanostack gen api, so that the Go types match the spec",
		"%s.%s has no Go field with the json name %q", owner, p.name, p.name)
	return nil, false
}

// fieldByJSON returns the field of st, or of a struct it embeds, whose json
// name is name.
func fieldByJSON(st *types.Struct, name string) *types.Var {
	for i := range st.NumFields() {
		f := st.Field(i)
		if jsonName(st.Tag(i)) == name {
			return f
		}
		if inner, ok := deref(f.Type()).Underlying().(*types.Struct); ok && f.Embedded() {
			if v := fieldByJSON(inner, name); v != nil {
				return v
			}
		}
	}
	return nil
}

// jsonName returns the name of a json struct tag, or "".
func jsonName(tag string) string {
	_, rest, ok := strings.Cut(tag, `json:"`)
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, `"`)
	name, _, _ = strings.Cut(name, ",")
	return name
}

// filters binds the properties of the filter object to the filters: A3,
// A4, and A9 for the lists and terms.
func (a *apiCheck) filters(fp *apiProp) {
	if fp == nil {
		for _, bf := range a.b.filters {
			if !bf.f.goOnly {
				a.errorf(bf.f.node, "add a filter object to "+a.name+", or wrap the filter in nanorepo.GoOnly(...)",
					"search %s: filter %s binds an API filter property, and %s has no filter object", a.s.name,
					bf.f.name, a.name)
			}
		}
		return
	}
	a.read[propFilter] = true // the problems below are not A10's
	v, ok := a.field(a.bodyStruct(), a.name, fp)
	if !ok {
		return
	}
	st, isStruct := deref(v.Type()).Underlying().(*types.Struct)
	if !isStruct {
		a.specErrorf(fp.line, "make filter an object schema", "%s.filter is %s, not an object", a.name,
			a.c.qualified(v.Type()))
		return
	}
	owner := a.name + ".filter"
	props := a.props(fp.schema)
	bound := map[string]string{} // property -> the filter that binds it
	for _, bf := range a.b.filters {
		if !bf.f.goOnly {
			a.bindFilter(bf, st, owner, props, bound)
		}
	}
	for _, p := range props {
		if bound[p.name] == "" && !a.hand[p.name] { // A3
			a.errorf(a.s.apiNode, "bind it with an operator, such as nanorepo.In.API(\""+p.name+"\"), or name it "+
				"with nanorepo.APIHandWritten(\""+p.name+"\") when the service resolves it",
				"%s: API filter property %q has no filter in search %s", a.name, p.name, a.s.name)
		}
	}
	if len(a.m.binds) > 0 {
		_, isPtr := types.Unalias(v.Type()).(*types.Pointer)
		a.m.filter = &apiField{name: v.Name(), ptr: isPtr}
	}
	a.read[propFilter] = len(props) > 0 // an unbound property is A3's; an empty object is A10's
}

// bodyStruct returns the struct of the body type.
func (a *apiCheck) bodyStruct() *types.Struct {
	st, _ := a.body.Underlying().(*types.Struct)
	return st
}

// bindFilter binds the API names of one filter's operators. A name that
// API or Range gives must be a property (A4); a conventional name binds
// when the property exists, and the filter must bind one property at least.
func (a *apiCheck) bindFilter(bf *builderField, st *types.Struct, owner string, props []apiProp,
	bound map[string]string,
) {
	f := bf.f
	found := false
	var tried []string
	for _, op := range filterOps(f) {
		names, explicit := apiNames(f, op)
		for i, name := range names {
			tried = append(tried, name)
			p := prop(props, name)
			if p == nil {
				if explicit { // A4
					a.errorf(op.node, "add "+name+" to "+owner+", or wrap the filter in nanorepo.GoOnly(...)",
						"%s.%s binds API filter property %q, which %s does not have", f.name, opLabel(op), name,
						owner)
				}
				continue
			}
			found = true
			switch {
			case a.hand[name]:
				a.errorf(a.s.handNode, "remove "+name+" from APIHandWritten: filter "+f.name+" binds it",
					"search %s: filter %s binds %q, and APIHandWritten names it too", a.s.name, f.name, name)
				continue
			case bound[name] != "":
				a.errorf(f.node, "bind "+name+" to one filter: name the other with .API(...)",
					"search %s: filters %s and %s both bind %s.%s", a.s.name, bound[name], f.name, owner, name)
				continue
			}
			bound[name] = f.name
			if b, ok := a.bind(bf, op, i, st, owner, p); ok {
				a.m.binds = append(a.m.binds, b)
			}
		}
	}
	if !found && len(tried) > 0 && !explicitOnly(f) { // A4, conventional names
		a.errorf(f.node, "name the property with .API(\"...\") on the operator, add one of "+strings.Join(tried, ", ")+
			" to "+owner+", or wrap the filter in nanorepo.GoOnly(...)",
			"search %s: filter %s binds no property of %s (it looks for %s)", a.s.name, f.name, owner,
			strings.Join(tried, ", "))
	}
}

// explicitOnly reports whether every operator of f names its properties,
// so that A4 has reported each missing one.
func explicitOnly(f *searchFilter) bool {
	for _, op := range filterOps(f) {
		if _, explicit := apiNames(f, op); !explicit {
			return false
		}
	}
	return true
}

// filterOps returns the operators of f. A Flag has none in the
// declaration, and binds one property with Is.
func filterOps(f *searchFilter) []searchOp {
	if f.kind == "Flag" {
		return []searchOp{{name: "Is"}}
	}
	return f.ops
}

// opLabel names an operator in a message: In, or Range.
func opLabel(op searchOp) string { return op.name }

// apiNames returns the API filter properties of one operator of f, and
// whether the declaration names them (API or Range with names). Without
// names the convention of db/nanorepo applies: Eq the field, In, InFold
// and ContainsAny the plural, Contains <field>_contains, Present
// has_<field>, Range <field>_since and <field>_until, Flag the snake case
// of its name.
func apiNames(f *searchFilter, op searchOp) (names []string, explicit bool) {
	if len(op.api) > 0 {
		return op.api, true
	}
	base := snake(f.name)
	switch op.name {
	case "In", "InFold", "ContainsAny":
		return []string{pluralWord(base)}, false
	case "Contains":
		return []string{base + "_contains"}, false
	case "Present":
		return []string{"has_" + base}, false
	case "Range":
		return []string{base + "_since", base + "_until"}, false
	}
	return []string{base}, false // Eq, Is
}

// pluralWord is the plural of the last word of a snake case name:
// status -> statuses, category -> categories, id -> ids.
func pluralWord(s string) string {
	switch {
	case strings.HasSuffix(s, "ss"), strings.HasSuffix(s, "us"), strings.HasSuffix(s, "x"),
		strings.HasSuffix(s, "sh"), strings.HasSuffix(s, "ch"):
		return s + "es"
	case strings.HasSuffix(s, "s"):
		return s
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// opMethod returns the method of the builder field that an operator calls
// for its i-th API name, and whether the method takes a list.
func opMethod(op searchOp, i int) (string, bool) {
	switch op.name {
	case "In", "InFold":
		return "In", true
	case "ContainsAny":
		return "ContainsAny", true
	case "Contains":
		return "Contains", false
	case "Present":
		return "Present", false
	case "Is":
		return "Is", false
	case "Range":
		lower, upper := "Since", "Until"
		switch op.bounds {
		case "HalfOpen":
			upper = "Before"
		case "Exclusive":
			lower, upper = "After", "Before"
		}
		if i == 0 {
			return lower, false
		}
		return upper, false
	}
	return "Eq", false
}

// bind checks one property against the method it binds, and returns the
// bind: the value type (A9's limits too), a list for a list, and a pointer
// for a property that the client may leave out.
func (a *apiCheck) bind(bf *builderField, op searchOp, i int, st *types.Struct, owner string, p *apiProp) (
	apiBind, bool,
) {
	v, ok := a.field(st, owner, p)
	if !ok {
		return apiBind{}, false
	}
	method, list := opMethod(op, i)
	want := a.wantType(bf, method)
	t := types.Unalias(v.Type())
	field := apiField{name: v.Name()}
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		field.ptr, t = true, types.Unalias(ptr.Elem())
	}
	elem := t
	if sl, isSlice := t.(*types.Slice); isSlice {
		field.slice, elem = true, sl.Elem()
	}
	where := owner + "." + p.name
	switch {
	case list != field.slice:
		takes, fix := "a list", "make "+where+" an array, or bind it to another operator"
		if !list {
			takes, fix = "one value", "make "+where+" a single value, or bind it to another operator"
		}
		a.specErrorf(p.line, fix, "%s is %s, and %s.%s takes %s", where, a.c.qualified(v.Type()), bf.f.name,
			method, takes)
		return apiBind{}, false
	case want != nil && !types.Identical(elem, want):
		fix := "give " + where + " the x-go-type " + a.c.qualified(want)
		if bf.f.col != nil && (bf.kind == "In" || bf.kind == "Eq" || bf.kind == "EqIn") {
			fix += ", or declare the filter with nanorepo.FilterAs[" + a.c.qualified(elem) + "]"
		}
		a.specErrorf(p.line, fix, "%s holds %s, and %s.%s takes %s", where, a.c.qualified(elem), bf.f.name,
			method, a.c.qualified(want))
		return apiBind{}, false
	case !field.ptr && !field.slice && !p.required:
		a.specErrorf(p.line, "give "+where+" a pointer type (remove x-go-type-skip-optional-pointer)",
			"%s is an optional %s, and the mapper cannot tell a property that the client did not send",
			where, a.c.qualified(v.Type()))
		return apiBind{}, false
	}
	a.limits(where, p, method, list)
	return apiBind{accessor: bf.f.name, method: method, list: list, field: field}, true
}
