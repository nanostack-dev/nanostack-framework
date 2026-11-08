package genvalidate

import (
	"fmt"
	"go/types"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

func (g *generator) array(sc *Schema, x string, sl *types.Slice, p string, c ctx) string {
	var b strings.Builder
	if v := sc.MinItems; v != nil && *v > 0 {
		fmt.Fprintf(
			&b,
			"if len(%s) < %d {\nerrs.Add(%s, validate.CodeMinItems, %q)\n}\n",
			x,
			*v,
			p,
			"must have at least "+plural(*v, "item", "items"),
		)
	}
	if v := sc.MaxItems; v != nil {
		fmt.Fprintf(
			&b,
			"if len(%s) > %d {\nerrs.Add(%s, validate.CodeMaxItems, %q)\n}\n",
			x,
			*v,
			p,
			"must have at most "+plural(*v, "item", "items"),
		)
	}
	if sc.UniqueItems {
		fn := "validate.UniqueJSON"
		if plainComparable(sl.Elem()) {
			fn = "validate.Unique"
		}
		i, j := g.fresh("i"), g.fresh("j")
		fmt.Fprintf(
			&b,
			"if %s, %s, ok := %s(%s); !ok {\nerrs.Add(%s, validate.CodeUniqueItems, validate.DuplicateMessage(%s, %s))\n}\n",
			i,
			j,
			fn,
			x,
			p,
			i,
			j,
		)
	}
	if sc.Items != nil {
		i, pi := g.fresh("i"), g.fresh("f")
		nullable := g.isNullable(sc.Items)
		inner := g.value(sc.Items, x+"["+i+"]", sl.Elem(), pi, c.deeper())
		switch {
		case inner == "" && nullable:
		case inner == "":
			fmt.Fprintf(&b, "for %s := range %s {\nerrs.NotNull(%s.Index(%s))\n}\n", i, x, p, i)
		case nullable:
			// A null item satisfies the schema; the checks are for a value.
			fmt.Fprintf(&b, "for %s := range %s {\nif %s := %s.Index(%s); !%s.IsNull() {\n%s}\n}\n",
				i, x, pi, p, i, pi, inner)
		default:
			fmt.Fprintf(
				&b,
				"for %s := range %s {\nif %s := %s.Index(%s); errs.NotNull(%s) {\n%s}\n}\n",
				i,
				x,
				pi,
				p,
				i,
				pi,
				inner,
			)
		}
	}
	if len(sc.Types) > 0 && !slices.Contains(sc.Types, "array") {
		g.diag(sc.Loc, "schema type "+strings.Join(sc.Types, "|")+" does not match a Go slice")
	}
	return b.String()
}

// plainComparable reports whether == on t is JSON equality: comparable and
// free of pointers and interfaces.
func plainComparable(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return true
	case *types.Array:
		return plainComparable(u.Elem())
	case *types.Struct:
		for f := range u.Fields() {
			if !plainComparable(f.Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// bare drops the parentheses of "(*x)" where the operand does not need them:
// call arguments, comparisons and switch tags.
func bare(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") {
		return x[1 : len(x)-1]
	}
	return x
}

func (g *generator) basic(sc *Schema, x string, t types.Type, bt *types.Basic, p string) string {
	x = bare(x)
	info := bt.Info()
	switch {
	case info&types.IsString != 0:
		s := x
		if _, named := t.(*types.Named); named {
			s = "string(" + x + ")"
		}
		return g.stringChecks(sc, s, p)
	case info&types.IsNumeric != 0 && info&types.IsComplex == 0:
		if cs := stringConstraints(
			sc,
		); len(cs) > 0 && !slices.Contains(sc.Types, "integer") &&
			!slices.Contains(sc.Types, "number") {
			g.diag(sc.Loc, strings.Join(cs, ", ")+" not enforced on Go type "+types.TypeString(t, nil))
		}
		return g.numberChecks(sc, x, bt, p)
	case info&types.IsBoolean != 0:
		return g.enumChecks(sc, x, "boolean", p)
	}
	return ""
}

var formatChecks = map[string]struct{ fn, msg string }{
	"uuid":      {"validate.UUID", "must be a UUID"},
	"email":     {"validate.Email", "must be an email address"},
	"uri":       {"validate.URI", "must be an absolute URI"},
	"date-time": {"validate.DateTime", "must be an RFC 3339 date-time"},
	"date":      {"validate.Date", "must be an RFC 3339 full-date (YYYY-MM-DD)"},
	"ipv4":      {"validate.IPv4", "must be an IPv4 address"},
	"ipv6":      {"validate.IPv6", "must be an IPv6 address"},
	"hostname":  {"validate.Hostname", "must be a host name"},
}

// annotationFormats carry no assertion for a string value.
var annotationFormats = map[string]bool{
	"password": true,
	"binary":   true,
	"int32":    true,
	"int64":    true,
	"float":    true,
	"double":   true,
}

func (g *generator) stringChecks(sc *Schema, s, p string) string {
	var b strings.Builder
	minL, maxL := sc.MinLength, sc.MaxLength
	if minL != nil && *minL == 0 {
		minL = nil
	}
	if minL != nil || maxL != nil {
		g.use("unicode/utf8")
		n := g.fresh("n")
		var parts []string
		if minL != nil {
			parts = append(
				parts,
				fmt.Sprintf(
					"%s < %d {\nerrs.Add(%s, validate.CodeMinLength, %q)\n}",
					n,
					*minL,
					p,
					"must be at least "+plural(*minL, "character", "characters")+" long",
				),
			)
		}
		if maxL != nil {
			parts = append(
				parts,
				fmt.Sprintf(
					"%s > %d {\nerrs.Add(%s, validate.CodeMaxLength, %q)\n}",
					n,
					*maxL,
					p,
					"must be at most "+plural(*maxL, "character", "characters")+" long",
				),
			)
		}
		fmt.Fprintf(&b, "if %s := utf8.RuneCountInString(%s); %s\n", n, s, strings.Join(parts, " else if "))
	}
	if sc.Pattern != "" {
		if name, ok := g.pattern(sc.Loc, sc.Pattern); ok {
			fmt.Fprintf(
				&b,
				"if !%s.MatchString(%s) {\nerrs.Add(%s, validate.CodePattern, %q)\n}\n",
				name,
				s,
				p,
				"must match the pattern "+sc.Pattern,
			)
		}
	}
	if sc.Format != "" {
		if f, ok := formatChecks[sc.Format]; ok {
			fmt.Fprintf(&b, "if !%s(%s) {\nerrs.Add(%s, validate.CodeFormat, %q)\n}\n", f.fn, s, p, f.msg)
		} else if !annotationFormats[sc.Format] {
			g.diag(sc.Loc, "format "+sc.Format+" is not checked")
		}
	}
	b.WriteString(g.enumChecks(sc, s, "string", p))
	return b.String()
}

func (g *generator) numberChecks(sc *Schema, x string, bt *types.Basic, p string) string {
	var b strings.Builder
	bound := func(n *Number, op, code, msg string) {
		if n != nil {
			fmt.Fprintf(
				&b,
				"if %s {\nerrs.Add(%s, validate.%s, %q)\n}\n",
				compare(x, bt, op, n),
				p,
				code,
				msg+" "+n.Lit,
			)
		}
	}
	bound(sc.Minimum, "<", "CodeMinimum", "must be at least")
	bound(sc.ExclusiveMinimum, "<=", "CodeExclusiveMinimum", "must be greater than")
	bound(sc.Maximum, ">", "CodeMaximum", "must be at most")
	bound(sc.ExclusiveMaximum, ">=", "CodeExclusiveMaximum", "must be less than")
	if m := sc.MultipleOf; m != nil {
		cond := fmt.Sprintf("!validate.MultipleOf(%s, %q)", x, m.Lit)
		if bt.Info()&types.IsInteger != 0 && m.IsInt() && fits(bt, m.Rat.Num()) {
			cond = fmt.Sprintf("%s%%%s != 0", x, m.Rat.Num().String())
		}
		fmt.Fprintf(
			&b,
			"if %s {\nerrs.Add(%s, validate.CodeMultipleOf, %q)\n}\n",
			cond,
			p,
			"must be a multiple of "+m.Lit,
		)
	}
	if bt.Info()&types.IsInteger != 0 {
		b.WriteString(g.enumChecks(sc, x, "integer:"+bt.Name(), p))
	} else {
		b.WriteString(g.enumChecks(sc, x, "number", p))
	}
	return b.String()
}

// compare renders x op n so that it compiles for x's type: a fractional or
// out-of-range bound on an integer compares as float64.
func compare(x string, bt *types.Basic, op string, n *Number) string {
	if bt.Info()&types.IsInteger != 0 {
		if n.IsInt() && fits(bt, n.Rat.Num()) {
			return fmt.Sprintf("%s %s %s", x, op, n.Rat.Num().String())
		}
		f, _ := n.Rat.Float64()
		return fmt.Sprintf("float64(%s) %s %s", x, op, floatLit(f))
	}
	f, _ := n.Rat.Float64()
	return fmt.Sprintf("%s %s %s", x, op, floatLit(f))
}

func floatLit(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// fits reports whether v is representable in the integer type bt.
func fits(bt *types.Basic, v *big.Int) bool {
	var bits uint
	unsigned := bt.Info()&types.IsUnsigned != 0
	switch bt.Kind() {
	case types.Int8, types.Uint8:
		bits = 8
	case types.Int16, types.Uint16:
		bits = 16
	case types.Int32, types.Uint32:
		bits = 32
	default:
		bits = 64
	}
	if unsigned {
		return v.Sign() >= 0 && v.BitLen() <= int(bits)
	}
	lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
	hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
	return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}
