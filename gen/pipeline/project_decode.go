package pipeline

import (
	"fmt"
	"go/ast"
	"reflect"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// decodeMap reads a map literal whose keys are constants, such as
// map[string]string{"organization_id": "Org"}.
func (d *decoder) decodeMap(expr ast.Expr, v reflect.Value, path string) {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return
	}
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		d.notLiteral(expr, path, "map literal")
		return
	}
	m := reflect.MakeMapWithSize(v.Type(), len(lit.Elts))
	for _, e := range lit.Elts {
		kv, isPair := e.(*ast.KeyValueExpr)
		if !isPair {
			d.problems = append(d.problems, gen.Errorf(projectFile, d.line(e), "write each entry as key: value",
				"%s has a value without a key", path))
			continue
		}
		key := reflect.New(v.Type().Key()).Elem()
		val := reflect.New(v.Type().Elem()).Elem()
		d.decode(kv.Key, key, path+" key")
		d.decode(kv.Value, val, path+"["+fmt.Sprint(key)+"]")
		m.SetMapIndex(key, val)
	}
	v.Set(m)
}

// decodeSlice reads a slice literal.
func (d *decoder) decodeSlice(expr ast.Expr, v reflect.Value, path string) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		d.notLiteral(expr, path, "slice literal")
		return
	}
	s := reflect.MakeSlice(v.Type(), len(lit.Elts), len(lit.Elts))
	for i, e := range lit.Elts {
		d.decode(e, s.Index(i), fmt.Sprintf("%s[%d]", path, i))
	}
	v.Set(s)
}
