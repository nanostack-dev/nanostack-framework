package genvalidate

import (
	"fmt"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/oas"
)

// ctx carries the state of one walk down a schema and a Go type.
type ctx struct {
	depth int
	// external is set inside a type of another package (x-go-type): a
	// missing field there is a diagnostic, not an error.
	external bool
}

func (c ctx) deeper() ctx { c.depth++; return c }

const maxInlineDepth = 24

const validatePathType = "github.com/nanostack-dev/nanostack-framework/core/validate.Path"

// value returns the statements that check the Go expression x, of type t,
// against sc, recording violations at the path expression p into errs. The
// caller has dealt with presence; x may still be a pointer or a Nullable.
func (g *generator) value(sc *oas.Schema, x string, t types.Type, p string, c ctx) string {
	if sc == nil {
		return ""
	}
	if c.depth > maxInlineDepth {
		g.diag(sc.Loc, "nested too deeply to inline; deeper values are not checked")
		return ""
	}
	t = types.Unalias(t)
	if code, wrapped := g.unwrap(sc, x, t, p, c); wrapped {
		return code
	}
	if sc.Ref != "" {
		code, target := g.reference(sc, x, t, p, c)
		if target == nil {
			return code
		}
		sc = target
	}

	var b strings.Builder
	for _, part := range sc.AllOf {
		b.WriteString(g.value(part, x, t, p, c.deeper()))
	}
	for _, k := range sc.Unsupported {
		g.diag(sc.Loc, k+" is not enforced")
	}
	if single := g.singleVariant(sc); single != nil && !g.unionWithVariants(t, 1) {
		// oneOf/anyOf [X, null] is how 3.1 makes a reference nullable;
		// oapi-codegen generates *X for it.
		b.WriteString(g.value(single, x, t, p, c.deeper()))
		rest := *sc
		rest.OneOf, rest.AnyOf, rest.Discriminator = nil, nil, nil
		sc = &rest
	}
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg() != g.gp.pkg {
		code, done := g.foreign(sc, x, n, p)
		b.WriteString(code)
		if done {
			return b.String()
		}
		c.external = true
	}
	b.WriteString(g.byKind(sc, x, t, p, c))
	return b.String()
}

// unwrap handles a pointer or a nullable.Nullable around the value: the
// checks run on the element when it is set.
func (g *generator) unwrap(sc *oas.Schema, x string, t types.Type, p string, c ctx) (string, bool) {
	if ptr, ok := t.(*types.Pointer); ok {
		inner := g.value(sc, "(*"+x+")", ptr.Elem(), p, c)
		if inner == "" {
			return "", true
		}
		return fmt.Sprintf("if %s != nil {\n%s}\n", x, inner), true
	}
	arg, ok := nullableArg(t)
	if !ok {
		return "", false
	}
	v := g.fresh("v")
	inner := g.value(sc, v, arg, p, c)
	switch {
	case inner == "":
		return "", true
	case !uses(inner, v):
		return fmt.Sprintf("if %s.IsSpecified() && !%s.IsNull() {\n%s}\n", x, x, inner), true
	}
	return fmt.Sprintf("if %s.IsSpecified() && !%s.IsNull() {\n%s := %s.MustGet()\n%s}\n", x, x, v, x, inner), true
}

// reference handles a $ref: a call to the Validate method of the named type
// that checks the target, or, when the caller must inline, the target.
func (g *generator) reference(sc *oas.Schema, x string, t types.Type, p string, c ctx) (string, *oas.Schema) {
	if sib := sc.Siblings(); sib != nil {
		return g.value(&oas.Schema{Loc: sc.Loc, Ref: sc.Ref}, x, t, p, c) + g.value(sib, x, t, p, c), nil
	}
	target := g.spec.Resolve(sc)
	if target == nil {
		return "", nil
	}
	if n := g.localNamed(t); n != nil && g.claim(n, target) {
		return fmt.Sprintf("%s.validateInto(%s, errs)\n", recv(x), p), nil
	}
	return "", target
}

// foreign handles a named type of another package: its own Validate method,
// or a type whose format decoding already enforced. done is false when the
// caller should inline the checks over the type's structure.
func (g *generator) foreign(sc *oas.Schema, x string, n *types.Named, p string) (code string, done bool) {
	if hasValidateMethod(n) {
		return fmt.Sprintf("errs.Append(%s.Validate(%s))\n", recv(x), p), true
	}
	if implied, ok := opaqueTypes[n.Obj().Pkg().Path()+"."+n.Obj().Name()]; ok {
		g.opaque(sc, n, implied)
		return "", true
	}
	return "", false
}

func (g *generator) byKind(sc *oas.Schema, x string, t types.Type, p string, c ctx) string {
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return g.object(sc, x, t, u, p, c)
	case *types.Map:
		return g.mapObject(sc, x, u, p, c)
	case *types.Slice:
		return g.array(sc, x, u, p, c)
	case *types.Basic:
		return g.basic(sc, x, t, u, p)
	case *types.Interface:
		return g.untyped(sc, x, p)
	}
	if hasAnyConstraint(sc) {
		g.diag(sc.Loc, "constraints are not enforced on Go type "+types.TypeString(t, nil))
	}
	return ""
}

// opaqueTypes are Go types oapi-codegen uses for string formats. Decoding
// already enforces the format; other string constraints cannot be checked.
var opaqueTypes = map[string]string{
	"time.Time":                   "date-time",
	"github.com/google/uuid.UUID": "uuid",
	"github.com/oapi-codegen/runtime/types.UUID": "uuid",
	"github.com/oapi-codegen/runtime/types.Date": "date",
	"github.com/oapi-codegen/runtime/types.File": "binary",
	"encoding/json.RawMessage":                   "",
}

func (g *generator) opaque(sc *oas.Schema, n *types.Named, implied string) {
	var lost []string
	if sc.Format != "" && sc.Format != implied {
		lost = append(lost, "format "+sc.Format)
	}
	for _, k := range stringConstraints(sc) {
		if k != "format" {
			lost = append(lost, k)
		}
	}
	if len(lost) > 0 {
		g.diag(sc.Loc, strings.Join(lost, ", ")+" not enforced on Go type "+n.Obj().Pkg().Name()+"."+n.Obj().Name())
	}
}

func hasValidateMethod(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Validate")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, isSig := fn.Type().(*types.Signature)
	return isSig && sig.Params().Len() == 1 && sig.Results().Len() == 1 &&
		types.TypeString(sig.Params().At(0).Type(), nil) == validatePathType
}

// recv turns "(*x)" back into "x" for a method call, which dereferences
// implicitly.
func recv(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") {
		return x[2 : len(x)-1]
	}
	return x
}

func nullableArg(t types.Type) (types.Type, bool) {
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "github.com/oapi-codegen/nullable" ||
		n.Obj().Name() != "Nullable" {
		return nil, false
	}
	return n.TypeArgs().At(0), true
}

// uses reports whether code refers to the identifier name.
func uses(code, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(code)
}

// bindOrBlank returns name if code uses it, else the blank identifier.
func bindOrBlank(code, name string) string {
	if uses(code, name) {
		return name
	}
	return "_"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
