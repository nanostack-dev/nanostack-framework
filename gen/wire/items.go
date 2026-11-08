package wire

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// nanoCallee returns the name of the core/nano function call invokes, or "".
func (w *walker) nanoCallee(pkg *packages.Package, call *ast.CallExpr) string {
	id := baseIdent(call.Fun)
	if id == nil {
		return ""
	}
	fn, ok := pkg.TypesInfo.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != NanoPath {
		return ""
	}
	return fn.Name()
}

// baseIdent returns the identifier naming a function in f, x.f, f[T] or x.f[T].
func baseIdent(e ast.Expr) *ast.Ident {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.IndexExpr:
		return baseIdent(e.X)
	case *ast.IndexListExpr:
		return baseIdent(e.X)
	}
	return nil
}

func (w *walker) next() int { return len(w.m.items) }

func (w *walker) add(p *provider, owned bool) {
	if p != nil {
		p.owned = owned
		w.m.items = append(w.m.items, p)
	}
}

// item reads one manifest or module entry.
func (w *walker) item(pkg *packages.Package, e ast.Expr) {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok && w.nanoItem(pkg, call) {
		return
	}
	if id := baseIdent(e); id != nil {
		if v, ok := pkg.TypesInfo.Uses[id].(*types.Var); ok && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			w.moduleVar(v, e.Pos())
			return
		}
	}
	w.d.add(w.at(e.Pos()), "use nano.Providers, nano.Bind, nano.Collect, nano.Invoke, nano.Module or a module variable",
		"unsupported manifest entry %s", exprString(e))
}

// nanoItem reads a call to a core/nano manifest function and reports whether
// call was one.
func (w *walker) nanoItem(pkg *packages.Package, call *ast.CallExpr) bool {
	owned := pkg == w.m.root
	switch w.nanoCallee(pkg, call) {
	case "Providers":
		for _, arg := range call.Args {
			w.add(w.function(pkg, arg, kindFunc), owned)
		}
	case "Module":
		for _, arg := range call.Args[1:] {
			w.item(pkg, arg)
		}
	case "Bind":
		w.add(w.bind(pkg, call), owned)
	case "Collect":
		w.collect(pkg, call, owned)
	case "Invoke":
		if len(call.Args) == 1 {
			w.add(w.function(pkg, call.Args[0], kindInvoke), owned)
		}
	default:
		return false
	}
	return true
}

// moduleVar reads the initializer of a package-level module variable.
func (w *walker) moduleVar(v *types.Var, ref token.Pos) {
	if !w.isNanoType(v.Type(), "Item") {
		w.d.add(w.at(ref), "list modules declared with nano.Module", "%s.%s is a %s, not a nano.Item",
			v.Pkg().Name(), v.Name(), v.Type())
		return
	}
	if w.modules[v] {
		w.d.add(w.at(ref), "remove the module from its own list, directly or through the modules it lists",
			"module %s.%s includes itself", v.Pkg().Name(), v.Name())
		return
	}
	pkg := w.pkgs[v.Pkg().Path()]
	if pkg == nil || len(pkg.Syntax) == 0 {
		w.needSyntax[v.Pkg().Path()] = true
		return
	}
	init := findVarInit(pkg, v)
	if init == nil {
		w.d.add(w.at(ref), "initialize the module variable with nano.Module(...) where it is declared",
			"cannot find the initializer of module %s.%s", v.Pkg().Name(), v.Name())
		return
	}
	w.modules[v] = true
	w.item(pkg, init)
	delete(w.modules, v)
}

// valueSpecs returns every package-level var spec of pkg.
func valueSpecs(files []*ast.File) []*ast.ValueSpec {
	var specs []*ast.ValueSpec
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, s := range gd.Specs {
				if vs, isVar := s.(*ast.ValueSpec); isVar {
					specs = append(specs, vs)
				}
			}
		}
	}
	return specs
}

func findVarInit(pkg *packages.Package, v *types.Var) ast.Expr {
	for _, vs := range valueSpecs(pkg.Syntax) {
		if len(vs.Values) != len(vs.Names) {
			continue
		}
		for i, name := range vs.Names {
			if pkg.TypesInfo.Defs[name] == v {
				return vs.Values[i]
			}
		}
	}
	return nil
}

func (w *walker) isNanoType(t types.Type, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == NanoPath && n.Obj().Name() == name
}

// function resolves a provider, collection member or invoke function
// reference.
func (w *walker) function(pkg *packages.Package, e ast.Expr, kind providerKind) *provider {
	what := "nano.Providers"
	switch kind {
	case kindMember:
		what = "nano.Collect"
	case kindInvoke:
		what = "nano.Invoke"
	case kindFunc, kindBind, kindCollect:
	}
	id := baseIdent(e)
	var fn *types.Func
	if id != nil {
		fn, _ = pkg.TypesInfo.Uses[id].(*types.Func)
	}
	if fn == nil || fn.Signature().Recv() != nil {
		w.d.add(w.at(e.Pos()), "list a package-level function, or wrap the value in one",
			"%s takes package-level functions; %s is not one", what, exprString(e))
		return nil
	}
	sig := fn.Signature()
	var targs []types.Type
	if inst, ok := pkg.TypesInfo.Instances[id]; ok {
		if s, isSig := inst.Type.(*types.Signature); isSig {
			sig = s
		}
		for t := range inst.TypeArgs.Types() {
			targs = append(targs, t)
		}
	} else if sig.TypeParams().Len() > 0 {
		w.d.add(w.at(e.Pos()), "instantiate it: "+funcName(fn)+"[T]",
			"%s: generic function %s needs explicit type arguments", what, funcName(fn))
		return nil
	}
	if sig.Variadic() {
		w.d.add(w.at(e.Pos()), "take a slice parameter, or wrap the function in one with fixed parameters",
			"%s: %s is variadic; providers take one value per parameter", what, funcName(fn))
		return nil
	}
	p := &provider{kind: kind, ref: e.Pos(), order: w.next(), fn: fn, targs: targs}
	for v := range sig.Params().Variables() {
		p.params = append(p.params, v.Type())
	}
	if !w.results(p, sig.Results()) {
		return nil
	}
	return p
}

// results checks a function's results against its kind's signature rule and
// records what it returns.
func (w *walker) results(p *provider, res *types.Tuple) bool {
	if p.kind == kindInvoke {
		switch {
		case res.Len() == 0:
		case res.Len() == 1 && isError(res.At(0).Type()):
			p.errRet = true
		default:
			w.d.add(w.at(p.ref), "return nothing, or only an error",
				"nano.Invoke: %s must return nothing or error; got %s", funcName(p.fn), w.m.tupleString(res))
			return false
		}
		return true
	}
	ok := false
	switch res.Len() {
	case 1:
		ok = !isError(res.At(0).Type())
	case 2:
		ok = isError(res.At(1).Type())
		p.errRet = true
	case 3:
		ok = isCleanup(res.At(1).Type()) && isError(res.At(2).Type())
		p.errRet, p.clean = true, true
	}
	if !ok {
		w.d.add(w.at(p.ref), "return T, (T, error) or (T, func(), error)",
			"provider %s returns %s", funcName(p.fn), w.m.tupleString(res))
		return false
	}
	p.out = res.At(0).Type()
	return true
}

func (w *walker) bind(pkg *packages.Package, call *ast.CallExpr) *provider {
	id := baseIdent(call.Fun)
	inst, ok := pkg.TypesInfo.Instances[id]
	if !ok || inst.TypeArgs.Len() != 2 {
		w.d.add(w.at(call.Pos()), "write nano.Bind[Interface, Implementation]()", "nano.Bind needs two type arguments")
		return nil
	}
	iface, impl := inst.TypeArgs.At(0), inst.TypeArgs.At(1)
	p := &provider{kind: kindBind, ref: call.Pos(), order: w.next(), out: iface, impl: impl, params: []types.Type{impl}}
	label := w.m.bindLabel(p)
	it, isIface := iface.Underlying().(*types.Interface)
	if !isIface {
		w.d.add(w.at(call.Pos()), "bind an interface type: nano.Bind[Interface, Implementation]()",
			"%s: %s is not an interface", label, w.m.typeString(iface))
		return nil
	}
	if !types.AssignableTo(impl, iface) {
		reason := ""
		fix := "implement the missing methods on " + w.m.typeString(impl) + ", or bind another type"
		if method, wrongType := types.MissingMethod(impl, it, true); method != nil {
			reason = fmt.Sprintf(" (missing method %s)", method.Name())
			if wrongType {
				reason = fmt.Sprintf(" (method %s has the wrong signature or receiver)", method.Name())
			}
			if decl := methodDecl(impl, method); decl != "" {
				fix = "give " + w.m.typeString(impl) + " the method " + decl + ", or bind another type"
			}
		}
		w.d.add(w.at(call.Pos()), fix,
			"%s: %s does not implement %s%s", label, w.m.typeString(impl), w.m.typeString(iface), reason)
		return nil
	}
	return p
}

// collect reads nano.Collect[T](members...): one kindCollect item declaring
// the collection, then one kindMember item per function.
func (w *walker) collect(pkg *packages.Package, call *ast.CallExpr, owned bool) {
	inst, ok := pkg.TypesInfo.Instances[baseIdent(call.Fun)]
	if !ok || inst.TypeArgs.Len() != 1 {
		w.d.add(w.at(call.Pos()), "write nano.Collect[T](providers...)", "nano.Collect needs the element type")
		return
	}
	elem := inst.TypeArgs.At(0)
	decl := &provider{kind: kindCollect, ref: call.Pos(), order: w.next(), elem: elem, out: types.NewSlice(elem)}
	w.add(decl, owned)
	for _, arg := range call.Args {
		p := w.function(pkg, arg, kindMember)
		if p == nil {
			continue
		}
		if !types.AssignableTo(p.out, elem) {
			w.d.add(
				w.at(arg.Pos()),
				"return a value assignable to "+w.m.typeString(elem)+", or move it out of the collection",
				"%s: %s returns %s, which is not assignable to %s",
				w.m.collectLabel(decl),
				funcName(p.fn),
				w.m.typeString(p.out),
				w.m.typeString(elem),
			)
			continue
		}
		p.elem = elem
		w.add(p, owned)
	}
}

func isError(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func isCleanup(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	return ok && sig.Params().Len() == 0 && sig.Results().Len() == 0 && !sig.Variadic()
}
