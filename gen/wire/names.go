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

	// The variables follow the fields, so a value's variable reads as its
	// field does and two values of one type name never fall back to a number.
	assignFields(e.g.order)
	local := maps.Clone(taken)
	for _, n := range e.g.order {
		n.varName = uniqueName(lowerCamel(n.field), local)
		n.needName = uniqueName("need"+n.field, local)
	}
}

// assignFields names the Overrides and Components fields. A name does not
// depend on the order of the graph; a new provider whose type shares a base
// name qualifies both fields. Every value whose type shares its base name
// with another one is qualified by its package name (*nanopg.DB and *sql.DB
// give NanopgDB and SQLDB). Values whose packages also share a name are
// qualified by their import paths instead (see pathQualifiers). Values that
// still collide, such as *T and T, or two instances of one generic type, are
// numbered in the order of their full type names. Collection members, named
// after their functions, come last.
func assignFields(order []*node) {
	byBase := map[string][]*node{}
	var members []*node
	for _, n := range order {
		if n.p != nil && n.p.kind == kindMember {
			members = append(members, n)
			continue
		}
		base := upperFirst(nodeBase(n))
		byBase[base] = append(byBase[base], n)
	}
	taken := map[string]bool{}
	byName := map[string][]*node{}
	for base, group := range byBase {
		if len(group) == 1 {
			byName[base] = append(byName[base], group[0])
			continue
		}
		for n, q := range qualifiers(group) {
			byName[q+base] = append(byName[q+base], n)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		group := byName[name]
		slices.SortFunc(group, func(a, b *node) int {
			return strings.Compare(types.TypeString(a.typ, nil), types.TypeString(b.typ, nil))
		})
		for _, n := range group {
			n.field = uniqueName(name, taken)
		}
	}
	for _, n := range members {
		n.field = uniqueName(upperFirst(nodeBase(n)), taken)
	}
}

// qualifiers qualifies each node of a group whose types share a base name:
// by its package name, or by its import path when another node's package has
// that name too.
func qualifiers(group []*node) map[*node]string {
	byQualifier := map[string][]*node{}
	for _, n := range group {
		q := qualifier(n.typ)
		byQualifier[q] = append(byQualifier[q], n)
	}
	out := map[*node]string{}
	for q, same := range byQualifier {
		paths := map[string]string{}
		for _, n := range same {
			paths[pkgPath(n.typ)] = q
		}
		if len(paths) > 1 {
			paths = pathQualifiers(slices.Collect(maps.Keys(paths)))
		}
		for _, n := range same {
			out[n] = paths[pkgPath(n.typ)]
		}
	}
	return out
}

// qualifier is the package name of t's named type, capitalised as a Go
// identifier: sql gives SQL, nanopg gives Nanopg. It is empty for a type
// without a package.
func qualifier(t types.Type) string {
	named := namedOf(sliceElem(t))
	if named == nil || named.Obj().Pkg() == nil {
		return ""
	}
	return identPart(named.Obj().Pkg().Name())
}

// pkgPath is the import path of t's named type, or "" for a type without a
// package.
func pkgPath(t types.Type) string {
	named := namedOf(sliceElem(t))
	if named == nil || named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path()
}

// pathQualifiers qualifies packages that share a name by their import paths.
// Each path gets the fewest last elements that no other path ends with (a
// version element counts with the one before it):
// example.com/billing/repository and example.com/invoice/repository give
// BillingRepository and InvoiceRepository; with example.com/repository too,
// that one gets ExampleComRepository. A path's qualifier depends only on the
// set of paths, never on their order. Two paths that give one identifier
// (a-b and a_b) are left to the numbering that follows.
func pathQualifiers(paths []string) map[string]string {
	split := map[string][]string{}
	for _, p := range paths {
		split[p] = pathElems(p)
	}
	out := map[string]string{}
	for _, p := range paths {
		elems := split[p]
		for k := 1; k <= len(elems); k++ {
			end := strings.Join(elems[len(elems)-k:], "/")
			if !slices.ContainsFunc(paths, func(o string) bool {
				oe := split[o]
				return o != p && strings.Join(oe[max(0, len(oe)-k):], "/") == end
			}) || k == len(elems) {
				out[p] = pathIdent(elems[len(elems)-k:])
				break
			}
		}
	}
	return out
}

// pathElems splits an import path at its slashes, keeping a version element
// (v2) with the one before it: example.com/kv/v2 gives example.com and kv/v2,
// so the major version of a module never stands for its package alone.
func pathElems(p string) []string {
	var elems []string
	for e := range strings.SplitSeq(p, "/") {
		if len(elems) > 0 && isVersion(e) {
			elems[len(elems)-1] += "/" + e
			continue
		}
		elems = append(elems, e)
	}
	return elems
}

// isVersion reports whether a path element is a version such as v2.
func isVersion(e string) bool {
	rest, ok := strings.CutPrefix(e, "v")
	return ok && rest != "" && strings.Trim(rest, "0123456789") == ""
}

// pathIdent joins import path elements into a Go identifier: each run of
// letters and digits starts with a capital, and an initialism is written in
// capitals (example.com/api/v2 gives ExampleComAPIV2).
func pathIdent(elems []string) string {
	var b strings.Builder
	for _, e := range elems {
		for _, part := range strings.FieldsFunc(e, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			b.WriteString(identPart(part))
		}
	}
	s := b.String()
	if r := []rune(s); len(r) == 0 || !unicode.IsLetter(r[0]) {
		s = "Pkg" + s // a path element that starts with a digit
	}
	return s
}

// identPart capitalises a package name or path element as part of a Go
// identifier: sql gives SQL, nanopg gives Nanopg.
func identPart(s string) string {
	if initialisms[strings.ToLower(s)] {
		return strings.ToUpper(s)
	}
	return upperFirst(s)
}

// sliceElem is the element type of a slice or array type, else t.
func sliceElem(t types.Type) types.Type {
	switch tt := types.Unalias(t).(type) {
	case *types.Slice:
		return tt.Elem()
	case *types.Array:
		return tt.Elem()
	}
	return t
}

// initialisms are the package names that Go writes in capitals inside an
// identifier.
var initialisms = map[string]bool{ //nolint:gochecknoglobals // A constant lookup table.
	"acl": true, "api": true, "dns": true, "grpc": true, "html": true, "http": true, "id": true, "ip": true,
	"json": true, "rpc": true, "sql": true, "ssh": true, "tcp": true, "tls": true, "udp": true, "ui": true,
	"uri": true, "url": true, "uuid": true, "xml": true,
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
// []http.Handler -> Handlers, and []log.ContextAttrs -> ContextAttrsList, as
// a plural of a name that ends in s reads as a typo.
func baseName(t types.Type) string {
	switch tt := types.Unalias(t).(type) {
	case *types.Named:
		return tt.Obj().Name()
	case *types.Pointer:
		return baseName(tt.Elem())
	case *types.Slice:
		return plural(baseName(tt.Elem()))
	case *types.Array:
		return plural(baseName(tt.Elem()))
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

func plural(name string) string {
	if strings.HasSuffix(name, "s") {
		return name + "List"
	}
	return name + "s"
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
