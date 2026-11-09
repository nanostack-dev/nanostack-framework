package genrepo

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// Each method of a bound repository interface resolves in this order
// (design D8 to D13):
//
//  1. hand-written: a method of the implementation in a file that nano gen
//     repo did not write, or promoted from an embedded field. It wins, and
//     its signature must match the interface;
//  2. derived: the name follows the grammar on the table, so the method is
//     generated on the implementation, as one call to a base helper;
//  3. otherwise an error at the interface method, with the signature to
//     write by hand and a derivable rename when one exists.

// resolution is the outcome of one binding.
type resolution struct {
	b         *binding
	derived   []*derivedMethod
	overrides []string // hand-written methods whose names also derive
	explain   Explanation
	recvPtr   bool   // generated methods take a pointer receiver
	via       string // "r." or "r.itemsBase." when the implementation embeds several bases
	problems  []error
	missing   []string        // methods with no implementation: compile errors they cause are not reported twice
	searches  []*searchMethod // generated methods that run a declared search
	takes     []string        // the builder types that a method of the interface takes, XSearch
	// incomplete is true when a method was skipped because its signature
	// does not type-check: the package's own compile error says why.
	incomplete bool
}

// Explanation is how each method of one repository interface resolves, as
// nano gen repo -explain prints it.
type Explanation struct {
	Iface   string // the interface, as the declaring package writes it
	Impl    string
	Table   string
	Scope   string // the scope columns, "" for an unscoped table
	Methods []ExplainedMethod
}

// ExplainedMethod is one method of an Explanation.
type ExplainedMethod struct {
	Name string
	How  string // "derived: SELECT ...", "hand-written file.go:12", "ERROR: ..."
	Note string // why a hand-written name would or would not derive, or why a method failed
}

// resolver resolves the methods of one binding.
type resolver struct {
	l      *loaded
	b      *binding
	t      *tableBase
	scope  []string        // Go fields of the table's scope columns
	old    map[string]bool // where-type methods of the base on disk, "itemsWhere.Email"
	nested bool            // inside a rename check: no nested suggestions
	res    *resolution
}

// resolve resolves every binding of d.
func (l *loaded) resolve(d *declarations, scopes []string) []*resolution {
	old := l.oldPredicates()
	out := make([]*resolution, 0, len(d.bindings))
	for _, b := range d.bindings {
		r := &resolver{l: l, b: b, t: b.table, scope: b.table.scopeFields(scopes), old: old,
			res: &resolution{b: b, via: "r."}}
		r.run(d)
		out = append(out, r.res)
	}
	return out
}

// run resolves each method of the interface, in source order, then checks
// that the implementation embeds the base its derived methods run on.
func (r *resolver) run(d *declarations) {
	cols := make([]string, len(r.scope))
	for i, f := range r.scope {
		cols[i] = r.sqlName(f)
	}
	r.res.explain = Explanation{Iface: r.qualified(r.b.iface), Impl: r.b.impl.Obj().Name(), Table: r.t.sqlName,
		Scope: strings.Join(cols, ", ")}
	hand, ptr, val := r.handWritten()
	r.res.recvPtr = ptr || !val
	for _, fn := range r.methods() {
		r.resolveMethod(d, fn, hand)
	}
	bases := r.embeddedBases(d)
	if len(bases) > 1 {
		r.res.via = "r." + r.t.baseName() + "."
	}
	if len(r.res.searches) > 0 && len(r.res.derived) == 0 && !slices.Contains(bases, r.t.baseName()) {
		r.res.problems = append(r.res.problems, gen.Errorf(r.b.file, r.b.line,
			fmt.Sprintf("embed it: add %s as a field of %s, set to %s(db) in the constructor",
				r.t.baseName(), r.b.impl.Obj().Name(), r.t.newName()),
			"%s runs on the generated %s, but %s does not embed it", r.res.searches[0].fn.Name(),
			r.t.baseName(), r.b.impl.Obj().Name()))
	}
	if len(r.res.derived) > 0 && !slices.Contains(bases, r.t.baseName()) {
		name := r.res.explain.Iface
		r.res.problems = append(r.res.problems, gen.Errorf(r.b.file, r.b.line,
			fmt.Sprintf("embed it: add %s as a field of %s, set to %s(db) in the constructor",
				r.t.baseName(), r.b.impl.Obj().Name(), r.t.newName()),
			"%s has derived methods, which run on the generated %s, but %s does not embed it",
			name, r.t.baseName(), r.b.impl.Obj().Name()))
	}
}

// line records the explanation of fn.
func (r *resolver) line(fn *types.Func, how, note string) {
	r.res.explain.Methods = append(r.res.explain.Methods, ExplainedMethod{Name: fn.Name(), How: how, Note: note})
}

// fail records a problem of fn at pos.
func (r *resolver) fail(pos token.Pos, fn *types.Func, fix, format string, args ...any) {
	file, line := r.l.position(r.l.pkg.Fset, pos)
	r.res.problems = append(r.res.problems, gen.Errorf(file, line, fix,
		"%s.%s: %s", r.b.iface.Obj().Name(), fn.Name(), fmt.Sprintf(format, args...)))
}

// methods returns the methods of the interface, embedded interfaces
// included, in source order: the interface's own file first, then the
// other files by name.
func (r *resolver) methods() []*types.Func {
	it, _ := r.b.iface.Underlying().(*types.Interface)
	fns := slices.Collect(it.Methods())
	fset := r.l.pkg.Fset
	own := fset.Position(r.b.iface.Obj().Pos()).Filename
	slices.SortStableFunc(fns, func(a, b *types.Func) int {
		pa, pb := fset.Position(a.Pos()), fset.Position(b.Pos())
		if (pa.Filename == own) != (pb.Filename == own) {
			if pa.Filename == own {
				return -1
			}
			return 1
		}
		return cmp.Or(strings.Compare(pa.Filename, pb.Filename), cmp.Compare(pa.Offset, pb.Offset))
	})
	return fns
}

// handWritten returns the methods declared on the implementation, read
// from the syntax of the package: the files nano gen repo wrote are empty
// in this load, so a method a previous run generated never counts (design
// D9, D10). ptr and val say which receivers the methods use.
func (r *resolver) handWritten() (methods map[string]*ast.FuncDecl, ptr, val bool) {
	methods = map[string]*ast.FuncDecl{}
	for _, f := range r.l.pkg.Syntax {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
				continue
			}
			name, star := recvType(fd.Recv.List[0].Type)
			if name != r.b.impl.Obj().Name() {
				continue
			}
			methods[fd.Name.Name] = fd
			ptr, val = ptr || star, val || !star
		}
	}
	return methods, ptr, val
}

// recvType returns the type name of a receiver and whether it is a
// pointer.
func recvType(e ast.Expr) (string, bool) {
	star := false
	if s, ok := e.(*ast.StarExpr); ok {
		e, star = s.X, true
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name, star
	}
	return "", star
}

// resolveHandWritten checks a hand-written method against the interface
// (design D11) and notes whether its name would derive.
func (r *resolver) resolveHandWritten(fn *types.Func, fd *ast.FuncDecl) {
	file, line := r.l.position(r.l.pkg.Fset, fd.Pos())
	how := fmt.Sprintf("hand-written %s:%d", file, line)
	if obj, ok := r.l.pkg.TypesInfo.Defs[fd.Name].(*types.Func); ok {
		switch {
		case invalidSig(obj.Type()):
			r.res.incomplete = true
		case !types.Identical(obj.Type(), fn.Type()):
			how += "  ERROR: signature differs from the interface"
			r.fail(fd.Pos(), fn, "change it to:"+codeIndent+r.signature(fn),
				"the hand-written method on *%s does not match %s: it is %s, the interface wants %s",
				r.b.impl.Obj().Name(), r.b.iface.Obj().Name(), r.qualified(obj.Type()), r.qualified(fn.Type()))
		}
	}
	r.line(fn, how, r.overrideNote(fn))
}

// overrideNote says, for -explain, what the name would do without the
// hand-written method, and records a hand-written method that derives.
func (r *resolver) overrideNote(fn *types.Func) string {
	m, fail := r.derive(fn)
	switch {
	case fail == nil:
		r.res.overrides = append(r.res.overrides, fn.Name())
		return "the name also derives (" + r.methodSQL(m) + "); the hand-written method wins"
	case !fail.grammar:
		return "the name would not derive: " + fail.msg
	}
	return ""
}

// promotion is a method of an embedded field of the implementation.
type promotion struct {
	fn    *types.Func // nil: the field itself has the method twice at one depth
	field *types.Var
}

// promoted finds name among the methods promoted from the embedded fields
// of the implementation, at the shallowest depth, as the compiler does. It
// looks at each field on its own (design D10). Two hits are an ambiguous
// selector, which the compiler refuses.
func (r *resolver) promoted(name string) []promotion {
	st, ok := r.b.impl.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	best := -1
	var hits []promotion
	for f := range st.Fields() {
		if !f.Embedded() {
			continue
		}
		lt := f.Type()
		if _, isPtr := types.Unalias(lt).(*types.Pointer); !isPtr {
			lt = types.NewPointer(lt) // addressable through *impl: pointer methods count
		}
		obj, index, _ := types.LookupFieldOrMethod(lt, false, r.l.pkg.Types, name)
		fn, isFn := obj.(*types.Func)
		if !isFn && (obj != nil || index == nil) {
			continue
		}
		switch depth := len(index); {
		case best == -1 || depth < best:
			best, hits = depth, []promotion{{fn, f}}
		case depth == best:
			hits = append(hits, promotion{fn, f})
		}
	}
	return hits
}

// resolvePromoted resolves a method promoted from an embedded field: one
// hit wins as a hand-written method does, and its signature is checked the
// same way.
func (r *resolver) resolvePromoted(fn *types.Func, hits []promotion) {
	impl := r.b.impl.Obj().Name()
	if len(hits) > 1 || hits[0].fn == nil {
		names := make([]string, len(hits))
		for i, h := range hits {
			names[i] = h.field.Name()
		}
		where := "embedded fields " + strings.Join(names, " and ")
		if len(hits) == 1 {
			where = "the embedded " + names[0] + ", through two of its own embedded fields"
		}
		r.fail(fn.Pos(), fn, fmt.Sprintf("write %s on *%s by hand to choose one, or remove it from all but one "+
			"field:%s%s", fn.Name(), impl, codeIndent, r.signature(fn)),
			"%s is promoted from %s at the same depth: the selector is ambiguous, so *%s has no %s",
			fn.Name(), where, impl, fn.Name())
		r.line(fn, "ERROR: ambiguous: promoted from "+strings.Join(names, " and "), "")
		return
	}
	h := hits[0]
	file, line := r.l.position(r.l.pkg.Fset, h.fn.Pos())
	how := fmt.Sprintf("hand-written: promoted from the embedded %s, %s:%d", h.field.Name(), file, line)
	switch {
	case invalidSig(h.fn.Type()):
		r.res.incomplete = true
	case !types.Identical(h.fn.Type(), fn.Type()):
		how += "  ERROR: signature differs from the interface"
		recv := r.qualified(signatureOf(h.fn).Recv().Type())
		r.fail(h.fn.Pos(), fn, "change it to:"+codeIndent+signatureOn("r "+recv, fn, r.qualified),
			"the method promoted from the embedded %s does not match %s: it is %s, the interface wants %s",
			h.field.Name(), r.b.iface.Obj().Name(), r.qualified(h.fn.Type()), r.qualified(fn.Type()))
	}
	r.line(fn, how, r.overrideNote(fn))
}

// unimplemented reports a method with no hand-written body whose name does
// not derive, with the signature to paste and the other fix.
func (r *resolver) unimplemented(fn *types.Func, fail *refusal) {
	impl := r.b.impl.Obj().Name()
	hand := fmt.Sprintf("write it by hand on *%s, in a non-generated file of package %s:%s%s",
		impl, r.l.pkg.Name, codeIndent, r.signature(fn))
	if fail.body != "" {
		hand += " {" + bodyBreak + fail.body + codeIndent + "}"
	}
	fix := hand
	switch {
	case fail.alt == "":
	case fail.altFirst:
		fix = fail.alt + ";" + orIndent + hand
	default:
		fix = hand + orIndent + fail.alt
	}
	r.fail(fn.Pos(), fn, fix, "not implemented: no hand-written method on *%s, and the name does not derive: %s",
		impl, fail.msg)
	r.res.missing = append(r.res.missing, fn.Name())
	r.line(fn, "ERROR: not implemented", fail.msg)
}

// signature renders the declaration to paste on the implementation.
func (r *resolver) signature(fn *types.Func) string {
	recv := "r " + r.b.impl.Obj().Name()
	if r.res.recvPtr {
		recv = "r *" + r.b.impl.Obj().Name()
	}
	return signatureOn(recv, fn, r.qualified)
}

// signatureOn renders the declaration of fn with the receiver recv, every
// parameter named.
func signatureOn(recv string, fn *types.Func, qualified func(types.Type) string) string {
	sig := signatureOf(fn)
	params := make([]string, 0, sig.Params().Len())
	for i := range sig.Params().Len() {
		p := sig.Params().At(i)
		typ := qualified(p.Type())
		if sig.Variadic() && i == sig.Params().Len()-1 {
			typ = "..." + strings.TrimPrefix(typ, "[]")
		}
		params = append(params, nameOr(p.Name(), i)+" "+typ)
	}
	res := qualified(sig.Results())
	if sig.Results().Len() == 1 && sig.Results().At(0).Name() == "" {
		res = strings.Trim(res, "()")
	}
	return fmt.Sprintf("func (%s) %s(%s) %s", recv, fn.Name(), strings.Join(params, ", "), res)
}

// embeddedBases lists the generated bases the implementation embeds, from
// the syntax of its declaration: the bases do not exist in this load.
func (r *resolver) embeddedBases(d *declarations) []string {
	bases := map[string]bool{}
	for _, t := range d.tables {
		bases[t.baseName()] = true
	}
	var out []string
	if st := r.implStruct(); st != nil {
		for _, field := range st.Fields.List {
			if name, _ := recvType(field.Type); len(field.Names) == 0 && bases[name] {
				out = append(out, name)
			}
		}
	}
	return out
}

// implStruct returns the struct type the implementation is declared with.
func (r *resolver) implStruct() *ast.StructType {
	for _, f := range r.l.pkg.Syntax {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				if ts, isType := spec.(*ast.TypeSpec); isType && ts.Name.Name == r.b.impl.Obj().Name() {
					st, _ := ts.Type.(*ast.StructType)
					return st
				}
			}
		}
	}
	return nil
}

// qualified renders t for a message and a paste-ready signature: the
// declaring package's names unqualified, others by package name.
func (r *resolver) qualified(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == r.l.pkg.Types {
			return ""
		}
		return p.Name()
	})
}

// invalidSig reports a signature that does not type-check, such as one
// with an undefined type.
func invalidSig(t types.Type) bool { return strings.Contains(types.TypeString(t, nil), "invalid type") }

// oldPredicates returns the where-type methods of the base on disk, such as
// "itemsWhere.Email", for a column that has its equality and In predicates:
// the columns the last run saw.
func (l *loaded) oldPredicates() map[string]bool {
	out := map[string]bool{}
	if len(l.oldBase) == 0 {
		return out
	}
	f, err := parser.ParseFile(token.NewFileSet(), FileName, l.oldBase, parser.SkipObjectResolution)
	if err != nil {
		return out
	}
	methods := map[string]bool{}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) == 1 {
			if name, _ := recvType(fd.Recv.List[0].Type); name != "" {
				methods[name+"."+fd.Name.Name] = true
			}
		}
	}
	for m := range methods {
		if methods[m+"In"] {
			out[m] = true
		}
	}
	return out
}
