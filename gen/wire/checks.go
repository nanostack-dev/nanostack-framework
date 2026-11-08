package wire

import (
	"go/types"
)

// implements reports whether a value of type t has iface's methods.
func (g *graph) implements(t types.Type, iface *types.Interface) bool {
	return types.Implements(t, iface)
}

func (g *graph) analyzeLifecycle(n *node) {
	m := g.m
	t := n.typ
	switch n.kind {
	case nodeFunc:
	case nodeCollect:
		// The elements of an override are new values: record what Manage
		// could find on them, without the value-receiver diagnostics.
		elem := n.p.elem
		n.life = lifecycle{
			dynamic: types.IsInterface(elem),
			start:   g.implements(elem, m.ifaces.starter),
			stop:    g.implements(elem, m.ifaces.stopper),
			run:     g.implements(elem, m.ifaces.runner),
			check:   g.implements(elem, m.ifaces.checker),
		}
		return
	case nodeBind, nodeBuiltin, nodeAmbiguous:
		return
	}
	if types.IsInterface(t) {
		n.life.dynamic = true
		return
	}
	check := func(iface *types.Interface, method string) bool {
		if g.implements(t, iface) {
			return true
		}
		if _, isPtr := t.(*types.Pointer); !isPtr && g.implements(types.NewPointer(t), iface) {
			g.d.add(m.relPos(n.p.ref), "provider %s returns %s by value, but its %s method has a pointer receiver; "+
				"return *%s so the App and every consumer share one value", funcName(n.p.fn), m.typeString(t), method,
				m.typeString(t))
		}
		return false
	}
	n.life.start = check(m.ifaces.starter, "Start")
	n.life.stop = check(m.ifaces.stopper, "Stop")
	n.life.run = check(m.ifaces.runner, "Run")
	n.life.check = check(m.ifaces.checker, "CheckHealth")
	n.life.logger = key(t) == "*log/slog.Logger"
}

// checkAccess reports provider functions and types that the output package
// cannot name.
func (g *graph) checkAccess() {
	m := g.m
	report := func(p *provider, what string) {
		g.d.add(m.relPos(p.ref), "%s is unexported, so package %s cannot use it; export it, "+
			"or generate into the manifest package with -out .", what, m.outName)
	}
	checkFunc := func(p *provider) {
		if p.fn != nil && !p.fn.Exported() && p.fn.Pkg().Path() != m.outPath {
			report(p, funcName(p.fn))
		}
		for _, t := range p.targs {
			if !accessible(t, m.outPath) {
				report(p, m.typeString(t))
			}
		}
	}
	for _, n := range g.order {
		checkFunc(n.p)
		if !accessible(n.typ, m.outPath) {
			report(n.p, m.typeString(n.typ))
		}
	}
	for _, inv := range g.invokes {
		checkFunc(inv.p)
	}
}

func accessible(t types.Type, outPath string) bool {
	switch t := t.(type) {
	case *types.Alias:
		return visible(t.Obj(), outPath) && accessible(types.Unalias(t), outPath)
	case *types.Named:
		return accessibleNamed(t, outPath)
	case *types.Pointer:
		return accessible(t.Elem(), outPath)
	case *types.Slice:
		return accessible(t.Elem(), outPath)
	case *types.Array:
		return accessible(t.Elem(), outPath)
	case *types.Chan:
		return accessible(t.Elem(), outPath)
	case *types.Map:
		return accessible(t.Key(), outPath) && accessible(t.Elem(), outPath)
	case *types.Signature:
		return accessibleTuple(t.Params(), outPath) && accessibleTuple(t.Results(), outPath)
	case *types.Struct:
		for f := range t.Fields() {
			if !visible(f, outPath) || !accessible(f.Type(), outPath) {
				return false
			}
		}
		return true
	case *types.Interface:
		for fn := range t.Methods() {
			if !visible(fn, outPath) {
				return false
			}
		}
		return true
	}
	return true
}

// visible reports whether package outPath can name obj.
func visible(obj types.Object, outPath string) bool {
	return obj.Pkg() == nil || obj.Exported() || obj.Pkg().Path() == outPath
}

func accessibleNamed(t *types.Named, outPath string) bool {
	if !visible(t.Obj(), outPath) {
		return false
	}
	for ta := range t.TypeArgs().Types() {
		if !accessible(ta, outPath) {
			return false
		}
	}
	return true
}

func accessibleTuple(t *types.Tuple, outPath string) bool {
	for v := range t.Variables() {
		if !accessible(v.Type(), outPath) {
			return false
		}
	}
	return true
}
