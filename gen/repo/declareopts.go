package genrepo

import (
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strings"
)

// This file reads the options of nanorepo.Implement: ToRow, which lets the
// writes of the interface derive, and OnUnique.

// writable reports whether the writes of b derive: the declaration maps a
// domain value to a row with ToRow, or its rows are the model (NoMapper).
func (b *binding) writable() bool {
	return b.toRow != nil || b.table.mapper == nil
}

// onUniqueRule is one nanorepo.OnUnique(target, constraints...): a unique
// violation of one of the constraints becomes target.
type onUniqueRule struct {
	target      *types.Var // a package-level error variable
	constraints []string
}

// options reads the options of nanorepo.Implement after toDomain: at most
// one ToRow, whose row and domain types are those of toDomain, and any
// number of OnUnique. Each is a direct call, so nanostack gen repo reads it
// from source.
func (c *collector) options(call *ast.CallExpr, b *binding, row, domain types.Type) bool {
	if call.Ellipsis.IsValid() {
		c.errorf(call, "pass each option as a call: nanorepo.ToRow(...), nanorepo.OnUnique(...)",
			"nanorepo.Implement takes its options as a slice; nanostack gen repo reads each option from source")
		return false
	}
	info := c.l.pkg.TypesInfo
	ok := true
	seen := map[string]bool{}
	for _, arg := range call.Args[2:] {
		opt, isCall := ast.Unparen(arg).(*ast.CallExpr)
		var fn *types.Func
		var sel *ast.SelectorExpr
		if isCall {
			fn, sel = calledFunc(info, opt)
		}
		if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != nanorepoPath {
			c.errorf(arg, "pass nanorepo.ToRow(...) or nanorepo.OnUnique(...) in the call itself",
				"the option %s is not a call to nanorepo.ToRow or nanorepo.OnUnique", c.source(arg))
			ok = false
			continue
		}
		switch fn.Name() {
		case "ToRow":
			if b.toRow != nil {
				c.errorf(arg, "keep one nanorepo.ToRow", "nanorepo.Implement passes nanorepo.ToRow twice")
				ok = false
				continue
			}
			ok = c.toRow(opt, sel, b, row, domain) && ok
		case "OnUnique":
			ok = c.onUnique(opt, b, seen) && ok
		}
	}
	return ok
}

// toRow checks nanorepo.ToRow(fn): fn maps the domain type of toDomain to
// its row type, and has no state (design D5, as toDomain).
func (c *collector) toRow(opt *ast.CallExpr, sel *ast.SelectorExpr, b *binding, row, domain types.Type) bool {
	if b.table.mapper == nil {
		c.errorf(opt, "remove nanorepo.ToRow: with NoMapper, the writes take the model "+c.qualified(row),
			"nanorepo.ToRow is passed with NoMapper")
		return false
	}
	inst, found := c.l.pkg.TypesInfo.Instances[sel.Sel]
	if !found || inst.TypeArgs.Len() != 2 || len(opt.Args) != 1 {
		c.errorf(opt, "write nanorepo.ToRow(domainToRow)", "nanorepo.ToRow does not type-check")
		return false
	}
	if gotRow, gotDomain := inst.TypeArgs.At(0), inst.TypeArgs.At(1); !types.Identical(gotRow, row) ||
		!types.Identical(gotDomain, domain) {
		c.errorf(opt, "pass a function from "+c.qualified(domain)+" to "+c.qualified(row)+", the other direction "+
			"of "+mapperText(b.table.mapper),
			"nanorepo.ToRow maps %s to %s, but the mapper %s maps %s to %s", c.qualified(gotDomain),
			c.qualified(gotRow), mapperText(b.table.mapper), c.qualified(row), c.qualified(domain))
		return false
	}
	ref, ok := c.mapper(opt.Args[0], row)
	if !ok {
		return false
	}
	if ref == nil {
		c.errorf(opt, "remove nanorepo.ToRow", "nanorepo.ToRow(NoMapper) maps nothing")
		return false
	}
	b.toRow = ref
	return true
}

// onUnique checks nanorepo.OnUnique(target, constraints...): target is a
// package-level error variable, and each constraint a string constant that
// names a unique key or a unique index of the table, once per declaration.
func (c *collector) onUnique(opt *ast.CallExpr, b *binding, seen map[string]bool) bool {
	info := c.l.pkg.TypesInfo
	if len(opt.Args) < 2 || opt.Ellipsis.IsValid() {
		c.errorf(opt, `write nanorepo.OnUnique(ErrExists, "table_column_key")`,
			"nanorepo.OnUnique needs an error and at least one constraint name")
		return false
	}
	target := c.errorVar(opt.Args[0])
	if target == nil {
		return false
	}
	t := b.table
	if !t.keysFound {
		c.errorf(opt, "run nanostack gen db, which writes UniqueKeys into the table package",
			"nanorepo.OnUnique names constraints of %s, and its table package has no UniqueKeys to check them",
			t.sqlName)
		return false
	}
	known := slices.Clone(t.notKeys)
	for _, k := range t.keys {
		known = append(known, k.name)
	}
	rule := onUniqueRule{target: target}
	for _, arg := range opt.Args[1:] {
		tv, isConst := info.Types[arg]
		if !isConst || tv.Value == nil || tv.Value.Kind() != constant.String {
			c.errorf(arg, "pass the constraint name as a string constant",
				"the constraint %s of nanorepo.OnUnique is not a string constant", c.source(arg))
			return false
		}
		name := constant.StringVal(tv.Value)
		switch {
		case !slices.Contains(known, name):
			slices.Sort(known)
			c.errorf(arg, "name one of "+strings.Join(known, ", ")+", or run nanostack gen db after the migration",
				"table %s has no unique key or unique index %q", t.sqlName, name)
			return false
		case seen[name]:
			c.errorf(arg, "name each constraint once", "nanorepo.OnUnique names %q twice", name)
			return false
		}
		seen[name] = true
		rule.constraints = append(rule.constraints, name)
	}
	b.onUnique = append(b.onUnique, rule)
	return true
}

// errorVar resolves the target of nanorepo.OnUnique: a package-level
// variable whose type implements error, of this package or another.
func (c *collector) errorVar(arg ast.Expr) *types.Var {
	info := c.l.pkg.TypesInfo
	var id *ast.Ident
	switch x := ast.Unparen(arg).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		if _, isPkg := info.Uses[identOf(x.X)].(*types.PkgName); isPkg {
			id = x.Sel
		}
	}
	var v *types.Var
	if id != nil {
		v, _ = info.Uses[id].(*types.Var)
	}
	errType, _ := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if v == nil || v.Parent() != v.Pkg().Scope() || !types.Implements(v.Type(), errType) {
		c.errorf(arg, "declare var ErrX = errors.New(...) at package level and pass its name",
			"the target %s of nanorepo.OnUnique is not a package-level error variable", c.source(arg))
		return nil
	}
	return v
}
