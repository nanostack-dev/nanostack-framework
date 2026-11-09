package genrepo

import (
	"slices"
	"strconv"
)

// computedVars writes the package variable of each SortExpr key: the
// spec's Keys and Columns name it.
func (e *emitter) computedVars(b *builder, row string) {
	for _, k := range computedKeys(b) {
		e.p("// %s orders on %s(), and the Row selects it as %s.\n", keyVar(b, k), e.fn(k.expr), k.computed)
		e.p("var %s = %s(%s, %s(), func(r %s) %s { return r.%s })", keyVar(b, k), e.ks(k.ctor),
			strconv.Quote(k.computed), e.fn(k.expr), row, e.typ(k.value), k.field)
		if k.nulls != "" {
			e.p(".%s()", k.nulls)
		}
		e.p("\n\n")
	}
}

// computedKeys are the SortExpr keys of b, in sort order.
func computedKeys(b *builder) []*sortKey {
	var out []*sortKey
	for _, sort := range b.sorts {
		if sort.key != nil && sort.key.expr != nil && !slices.Contains(out, sort.key) {
			out = append(out, sort.key)
		}
	}
	return out
}

// keyVar is the package variable of a SortExpr key: the spec's Keys and
// Columns name it, so the select list holds the key's own projection.
func keyVar(_ *builder, k *sortKey) string { return k.varName }
