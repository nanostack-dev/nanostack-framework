package wire

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// assignNames gives each import a unique local name, then each value,
// demand flag and override field a unique identifier.
func (e *emitter) assignNames() {
	e.reserved = map[string]bool{
		"ctx":     true,
		"cfg":     true,
		"ov":      true,
		"app":     true,
		"err":     true,
		"cleanup": true,
		"elem":    true,
	}
	if e.m.samePkg {
		// Generated code lives beside the manifest: avoid its package-level names.
		for _, name := range e.m.root.Types.Scope().Names() {
			e.reserved[name] = true
		}
	} else {
		for _, name := range []string{"Build", "BuildComponents", "Components", "Config", "Overrides"} {
			e.reserved[name] = true
		}
	}

	names := maps.Clone(e.std)
	for p, pkg := range e.used {
		names[p] = pkg.Name()
	}
	paths := slices.Collect(maps.Keys(names))
	// Standard library first, then by path, so aliases are stable.
	slices.SortFunc(paths, func(a, b string) int {
		if sa, sb := isStd(a), isStd(b); sa != sb {
			if sa {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	e.imports = importset.New(e.m.outPath)
	e.imports.Block(slices.Sorted(maps.Keys(e.reserved))...)
	taken := maps.Clone(e.reserved)
	for _, p := range paths {
		taken[e.imports.Add(p, names[p])] = true
	}

	local := maps.Clone(taken)
	fields := map[string]bool{}
	for _, n := range e.g.order {
		n.varName = uniqueName(lowerCamel(nodeBase(n)), local)
		n.needName = uniqueName("need"+upperFirst(n.varName), local)
		n.field = e.fieldName(n, fields)
	}
}

// nodeBase is the identifier a node's names derive from: its type's name,
// or for a collection member, whose type its siblings share, the function's
// name without a New or Provide prefix.
func nodeBase(n *node) string {
	if n.p == nil || n.p.kind != kindMember {
		return baseName(n.typ)
	}
	name := n.p.fn.Name()
	for _, prefix := range []string{"New", "Provide"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" {
			return rest
		}
	}
	return name
}

func (e *emitter) fieldName(n *node, taken map[string]bool) string {
	base := upperFirst(nodeBase(n))
	if n.p != nil && n.p.kind == kindMember {
		return uniqueName(base, taken)
	}
	if !taken[base] {
		taken[base] = true
		return base
	}
	if named := namedOf(n.typ); named != nil && named.Obj().Pkg() != nil {
		qualified := upperFirst(named.Obj().Pkg().Name()) + base
		if !taken[qualified] {
			taken[qualified] = true
			return qualified
		}
		base = qualified
	}
	return uniqueName(base, taken)
}

func uniqueName(base string, taken map[string]bool) string {
	name := base
	for i := 2; taken[name] || token.IsKeyword(name) || types.Universe.Lookup(name) != nil; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	taken[name] = true
	return name
}

func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// nilable reports whether nil is a valid "unset" value of t.
func nilable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Map, *types.Slice, *types.Signature, *types.Chan:
		return true
	}
	return false
}

func namedOf(t types.Type) *types.Named {
	for {
		switch tt := types.Unalias(t).(type) {
		case *types.Named:
			return tt
		case *types.Pointer:
			t = tt.Elem()
		default:
			return nil
		}
	}
}

// baseName derives an identifier from a type: *invoice.PGStore -> PGStore,
// []http.Handler -> Handlers.
func baseName(t types.Type) string {
	switch tt := types.Unalias(t).(type) {
	case *types.Named:
		return tt.Obj().Name()
	case *types.Pointer:
		return baseName(tt.Elem())
	case *types.Slice:
		return baseName(tt.Elem()) + "s"
	case *types.Array:
		return baseName(tt.Elem()) + "s"
	case *types.Map:
		return baseName(tt.Elem()) + "Map"
	case *types.Chan:
		return baseName(tt.Elem()) + "Chan"
	case *types.Signature:
		return "Func"
	case *types.Basic:
		return tt.Name()
	}
	return "Value"
}

// lowerCamel lowercases a leading initialism: PGStore -> pgStore, DB -> db,
// Service -> service.
func lowerCamel(s string) string {
	r := []rune(s)
	n := 0
	for n < len(r) && unicode.IsUpper(r[n]) {
		n++
	}
	switch {
	case n == 0:
		return s
	case n == 1 || n == len(r):
	case n+1 == len(r) && r[n] == 's': // a plural initialism: IDs -> ids
		n = len(r)
	default:
		n-- // keep the capital that starts the next word
	}
	for i := range n {
		r[i] = unicode.ToLower(r[i])
	}
	return string(r)
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
