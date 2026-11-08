package wire

import (
	"fmt"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

type nodeKind int

const (
	nodeFunc      nodeKind = iota // a provider function or a collection member
	nodeBind                      // nano.Bind
	nodeCollect                   // nano.Collect: the []T of its members
	nodeBuiltin                   // ctx, cfg, a Config field, app
	nodeAmbiguous                 // several Config fields share the type
)

type visitState int

const (
	unvisited visitState = iota
	visiting
	visited
)

// node is one provided value.
type node struct {
	key  string
	typ  types.Type
	kind nodeKind
	p    *provider // nodeFunc, nodeBind; nodeCollect: its first declaration
	expr string    // nodeBuiltin: the Go expression that yields the value
	via  string    // nodeBuiltin, nodeAmbiguous: what provides it, for messages

	members []*node     // nodeCollect: one nodeFunc per member, in declaration order
	decls   []*provider // nodeCollect: every nano.Collect[T] that declares it

	state     visitState
	deps      []*node // nodeFunc, nodeBind: one per provider parameter; nodeCollect: members
	consumers []*node // needed nodes that take this one
	invoked   bool    // an invoke takes this node
	root      bool    // a Starter or Runner provider: visited even when nothing takes it
	life      lifecycle

	varName, needName, field string // set by the emitter
}

// lifecycle is what Build registers for a value.
type lifecycle struct {
	start, stop, run, check bool
	dynamic                 bool // interface-typed: App.Manage decides at run time
	logger                  bool // *slog.Logger: App.SetLogger
}

// graph is the resolved provider graph.
type graph struct {
	m       *manifest
	nodes   map[string]*node
	order   []*node // needed provider, binding, member and collection nodes, dependencies first
	invokes []*invoke
	appCfg  string // expression for NewApp's AppConfig argument
	d       diags
}

type invoke struct {
	p    *provider
	deps []*node
}

// key identifies a type within the single types universe of one load.
func key(t types.Type) string { return types.TypeString(types.Unalias(t), nil) }

func resolve(m *manifest) (*graph, error) {
	g := &graph{m: m, nodes: map[string]*node{}, appCfg: "nano.AppConfig{}"}
	g.addBuiltins()
	g.addItems()
	if err := g.d.err(); err != nil {
		return nil, err
	}
	g.walk()
	if len(g.d.list) == 0 {
		// A missing provider or a cycle cuts the walk short, which would make
		// providers look unused; report those only for an otherwise sound graph.
		g.reportUnused()
	}
	for _, n := range g.order {
		g.analyzeLifecycle(n)
	}
	if !m.samePkg {
		g.checkAccess()
	}
	if err := g.d.err(); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *graph) addItems() {
	var coll *node // the collection of the last nano.Collect, for its members
	for _, p := range g.m.items {
		switch p.kind {
		case kindInvoke:
			g.invokes = append(g.invokes, &invoke{p: p})
		case kindFunc, kindBind:
			g.addProvider(p)
		case kindCollect:
			coll = g.addCollection(p)
		case kindMember:
			if coll != nil {
				member := &node{key: fmt.Sprintf("member#%d", p.order), typ: p.out, kind: nodeFunc, p: p}
				coll.members = append(coll.members, member)
			}
		}
	}
}

// walk visits the graph from the roots and invokes in declaration order, so
// the output order follows the manifest wherever dependencies allow.
func (g *graph) walk() {
	for _, n := range g.nodes {
		if n.kind == nodeFunc {
			n.root = g.implements(n.typ, g.m.ifaces.runner) || g.implements(n.typ, g.m.ifaces.starter)
		}
	}
	for _, p := range g.m.items {
		switch p.kind {
		case kindInvoke:
			inv := g.invokeFor(p)
			for _, t := range p.params {
				dep := g.lookup(t, p)
				inv.deps = append(inv.deps, dep)
				if dep != nil {
					dep.invoked = true
					g.visit(dep, nil)
				}
			}
		case kindFunc:
			if n := g.nodes[key(p.out)]; n != nil && n.p == p && n.root {
				g.visit(n, nil)
			}
		case kindBind, kindCollect, kindMember: // reached through their consumers
		}
	}
}

func (g *graph) invokeFor(p *provider) *invoke {
	for _, inv := range g.invokes {
		if inv.p == p {
			return inv
		}
	}
	panic("unreachable")
}

func (g *graph) addBuiltins() {
	m := g.m
	g.nodes["context.Context"] = &node{
		key:  "context.Context",
		kind: nodeBuiltin,
		expr: "ctx",
		via:  "Build's ctx argument",
	}
	appKey := key(m.appType)
	g.nodes[appKey] = &node{key: appKey, typ: m.appType, kind: nodeBuiltin, expr: "app", via: "Build (the App itself)"}
	if m.config == nil {
		return
	}
	cfgKey := key(m.config)
	g.nodes[cfgKey] = &node{key: cfgKey, typ: m.config, kind: nodeBuiltin, expr: "cfg", via: "Build's cfg argument"}
	st, ok := m.config.Underlying().(*types.Struct)
	if !ok {
		return // lookupConfig accepts struct types only
	}
	for f := range st.Fields() {
		if !f.Exported() && !m.samePkg {
			continue
		}
		g.addConfigField(f)
	}
}

func (g *graph) addConfigField(f *types.Var) {
	k := key(f.Type())
	via := "Config." + f.Name()
	if types.Identical(f.Type(), g.m.ifaces.appConfig) && g.appCfg == "nano.AppConfig{}" {
		g.appCfg = "cfg." + f.Name()
	}
	switch existing := g.nodes[k]; {
	case existing == nil:
		g.nodes[k] = &node{key: k, typ: f.Type(), kind: nodeBuiltin, expr: "cfg." + f.Name(), via: via}
	case existing.kind == nodeBuiltin && strings.HasPrefix(existing.via, "Config."):
		existing.kind, existing.via = nodeAmbiguous, existing.via+" and "+via
	case existing.kind == nodeAmbiguous:
		existing.via += " and " + via
	}
}

func (g *graph) addProvider(p *provider) {
	m := g.m
	k := key(p.out)
	if existing := g.nodes[k]; existing != nil {
		switch existing.kind {
		case nodeBuiltin, nodeAmbiguous:
			g.d.add(m.at(p.ref), "remove the provider", "%s provides %s, which %s already provides",
				m.providerLabel(p), m.typeString(p.out), existing.via)
		case nodeCollect:
			g.d.add(m.at(p.ref), "remove the provider, or make it a member of the collection",
				"%s provides %s, which %s (%s) already provides", m.providerLabel(p), m.typeString(p.out),
				m.providerLabel(existing.p), m.relPos(existing.p.ref))
		case nodeFunc, nodeBind:
			g.d.add(m.at(p.ref), "keep one", "duplicate provider for %s: %s (%s) and %s (%s)", m.typeString(p.out),
				m.providerLabel(existing.p), m.relPos(existing.p.ref), m.providerLabel(p), m.relPos(p.ref))
		}
		return
	}
	kind := nodeFunc
	if p.kind == kindBind {
		kind = nodeBind
	}
	g.nodes[k] = &node{key: k, typ: p.out, kind: kind, p: p}
}

// addCollection returns the node of the []T that p declares. Every
// nano.Collect[T] of one T shares it, so modules can each add members.
func (g *graph) addCollection(p *provider) *node {
	k := key(p.out)
	existing := g.nodes[k]
	switch {
	case existing == nil:
		n := &node{key: k, typ: p.out, kind: nodeCollect, p: p, decls: []*provider{p}}
		g.nodes[k] = n
		return n
	case existing.kind == nodeCollect:
		existing.decls = append(existing.decls, p)
		return existing
	}
	g.addProvider(p) // reports the conflict
	return nil
}

// lookup finds the node providing t for consumer, reporting why when none does.
func (g *graph) lookup(t types.Type, consumer *provider) *node {
	m := g.m
	n := g.nodes[key(t)]
	if n != nil && n.kind != nodeAmbiguous {
		return n
	}
	at := m.relPos(consumer.ref)
	if consumer.kind != kindBind {
		at = m.declPos(consumer.fn)
	}
	if n != nil {
		g.d.add(
			m.at(consumer.ref),
			"take Config and pick a field, or give each field its own type",
			"%s needs %s, which more than one Config field provides (%s)",
			m.providerLabel(consumer),
			m.typeString(t),
			n.via,
		)
		return nil
	}
	g.d.add(m.at(consumer.ref), g.suggest(t), "no provider for %s (needed by %s at %s)",
		m.typeString(t), m.providerLabel(consumer), at)
	return nil
}

func (g *graph) suggest(t types.Type) string {
	m := g.m
	if n := g.nodes[key(types.NewSlice(t))]; n != nil && n.kind == nodeCollect {
		return fmt.Sprintf("take %s; %s collects %s values", m.typeString(n.typ), m.providerLabel(n.p), m.typeString(t))
	}
	if iface, ok := t.Underlying().(*types.Interface); ok {
		return g.suggestInterface(t, iface)
	}
	var alt types.Type
	if ptr, ok := t.(*types.Pointer); ok {
		alt = ptr.Elem()
	} else {
		alt = types.NewPointer(t)
	}
	if n := g.nodes[key(alt)]; n != nil && n.kind == nodeFunc {
		return fmt.Sprintf("make the parameter and the provider agree; %s provides %s",
			g.m.providerLabel(n.p), m.typeString(alt))
	}
	hint := fmt.Sprintf("add a function returning %s to nano.Providers", m.typeString(t))
	if m.config != nil {
		hint += " or a field of that type to Config"
	}
	return hint
}

func (g *graph) visit(n *node, path []*node) {
	if n.kind == nodeBuiltin || n.kind == nodeAmbiguous || n.state == visited {
		return
	}
	n.state = visiting
	path = append(path, n)
	n.deps = g.depsOf(n)
	for _, dep := range n.deps {
		if dep == nil || dep.kind == nodeBuiltin {
			continue
		}
		if dep.state == visiting {
			g.reportCycle(path, dep)
			continue
		}
		g.visit(dep, path)
	}
	n.state = visited
	g.order = append(g.order, n)
	for _, dep := range n.deps {
		if dep != nil && dep.kind != nodeBuiltin {
			dep.consumers = append(dep.consumers, n)
		}
	}
}

func (g *graph) depsOf(n *node) []*node {
	if n.kind == nodeCollect {
		return n.members
	}
	deps := make([]*node, len(n.p.params))
	for i, t := range n.p.params {
		deps[i] = g.lookup(t, n.p)
	}
	return deps
}

func (g *graph) reportCycle(path []*node, back *node) {
	m := g.m
	start := 0
	for i, n := range path {
		if n == back {
			start = i
		}
	}
	var steps []string
	for _, n := range path[start:] {
		steps = append(steps, fmt.Sprintf("%s (%s, %s)", m.typeString(n.typ), m.providerLabel(n.p), m.relPos(n.p.ref)))
	}
	steps = append(steps, m.typeString(back.typ))
	g.d.add(m.at(back.p.ref), "break it with an interface, or by moving the shared part into its own provider",
		"dependency cycle: %s", strings.Join(steps, " -> "))
}

func (g *graph) reportUnused() {
	m := g.m
	for _, p := range m.items {
		if !p.owned {
			continue
		}
		n := g.nodes[key(p.out)]
		switch p.kind {
		case kindInvoke, kindMember: // an invoke always runs; a member goes with its collection
			continue
		case kindCollect:
			if n != nil && n.state != visited {
				g.d.add(
					m.at(p.ref),
					"remove it",
					"unused collection %s: no Runner, Starter or nano.Invoke depends on %s",
					m.collectLabel(p),
					m.typeString(p.out),
				)
			}
		case kindBind:
			if n != nil && n.p == p && n.state != visited {
				g.d.add(
					m.at(p.ref),
					"remove it",
					"unused binding %s: no Runner, Starter or nano.Invoke depends on %s",
					m.bindLabel(p),
					m.typeString(p.out),
				)
			}
		case kindFunc:
			if n != nil && n.p == p && n.state != visited {
				g.d.add(
					m.at(p.ref),
					"remove it from nano.Providers",
					"unused provider %s: no Runner, Starter or nano.Invoke depends on %s",
					funcName(p.fn),
					m.typeString(p.out),
				)
			}
		}
	}
}

// suggestInterface is the fix for an interface that no provider returns. A
// provider whose type implements it can be bound. A provider whose type has
// some of its methods lacks the others: the fix names the first, in full.
// A tag interface of nano gen api (ADR 0024) is returned by its handler's
// constructor, so its fix is that provider, not a Bind.
func (g *graph) suggestInterface(t types.Type, iface *types.Interface) string {
	m := g.m
	var binds, returns []string
	near := ""
	for _, p := range m.items {
		if p.kind != kindFunc || types.IsInterface(p.out) || g.isHandlers(p.out) {
			continue
		}
		if types.AssignableTo(p.out, t) {
			binds = append(binds, fmt.Sprintf("nano.Bind[%s, %s]()", m.typeString(t), m.typeString(p.out)))
			returns = append(returns, funcName(p.fn))
			continue
		}
		if near == "" && hasSomeMethod(p.out, iface) {
			if method, _ := types.MissingMethod(p.out, iface, true); method != nil {
				near = fmt.Sprintf("give %s the method %s", m.typeString(p.out), methodDecl(p.out, method))
			}
		}
	}
	if named, ok := t.(*types.Named); ok && gopkg.IsTagServer(m.fset, named.Obj()) {
		if len(returns) > 0 {
			return fmt.Sprintf("make %s return %s", strings.Join(returns, " or "), m.typeString(t))
		}
		fix := fmt.Sprintf("add a provider that returns %s to nano.Providers in %s", m.typeString(t),
			m.relFile(m.file))
		if near != "" {
			fix += "; " + near
		}
		return fix
	}
	if len(binds) > 0 {
		return "bind an implementation with " + strings.Join(binds, " or ")
	}
	if near != "" {
		return near + ", then bind it with nano.Bind[" + m.typeString(t) + ", Impl]()"
	}
	return fmt.Sprintf("add a provider of an implementation and bind it with nano.Bind[%s, Impl]()", m.typeString(t))
}

// isHandlers reports whether t is *Handlers of a tag split: it implements
// every tag interface by passing each call on, so it is never their
// handler.
func (g *graph) isHandlers(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Name() != "Handlers" {
		return false
	}
	return filepath.Base(g.m.fset.Position(named.Obj().Pos()).Filename) == gopkg.HandlersFile
}

// hasSomeMethod reports whether t has at least one method of iface.
func hasSomeMethod(t types.Type, iface *types.Interface) bool {
	for method := range iface.Methods() {
		if obj, _, _ := types.LookupFieldOrMethod(t, true, method.Pkg(), method.Name()); obj != nil {
			if _, isFunc := obj.(*types.Func); isFunc {
				return true
			}
		}
	}
	return false
}

// methodDecl is the declaration t needs for method, written as code in t's
// own package writes it.
func methodDecl(t types.Type, method *types.Func) string {
	base := t
	if p, ok := base.(*types.Pointer); ok {
		base = p.Elem()
	}
	var pkg *types.Package
	if named, ok := base.(*types.Named); ok {
		pkg = named.Obj().Pkg()
	}
	return gopkg.MethodDecl(t, method, pkg)
}
