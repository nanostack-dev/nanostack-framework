package genvalidate

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// goPackage is the type-checked package oapi-codegen generated.
type goPackage struct {
	pkg  *types.Package
	fset *token.FileSet
	// ops are the strict server operations, keyed "METHOD /path".
	ops   map[string]*goOperation
	order []string
}

// goOperation is one method of the generated StrictServerInterface.
type goOperation struct {
	Name    string       // Go operation name, e.g. CreateWebhook
	Request *types.Named // CreateWebhookRequestObject
	Pos     token.Pos
}

var opComment = regexp.MustCompile(`^\s*\(([A-Z]+) (\S+)\)\s*$`)

// loadPackage type-checks the package in dir without the generated output
// file, so a stale or missing validate.gen.go never blocks generation.
// Type errors are expected (the strict template calls methods this
// generator writes) and are ignored; the types are still complete.
func loadPackage(dir, output string) (*goPackage, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	absOut := filepath.Join(absDir, filepath.Base(output))
	overlay := map[string][]byte{}
	if _, statErr := os.Stat(absOut); statErr == nil {
		name, nameErr := gopkg.PackageName(absDir, filepath.Base(absOut))
		if nameErr != nil {
			return nil, nameErr
		}
		overlay[absOut] = []byte("package " + name + "\n")
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:     absDir,
		Overlay: overlay,
		Tests:   false,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load Go package %s: %w", dir, err)
	}
	if len(pkgs) != 1 || pkgs[0].Types == nil {
		return nil, fmt.Errorf("load Go package %s: no package", dir)
	}
	p := pkgs[0]
	for _, e := range p.Errors {
		// Type errors, including the compile errors go list reports, are
		// expected: the strict template calls the methods generated here.
		if e.Kind == packages.ParseError {
			return nil, fmt.Errorf("load Go package %s: %w", dir, e)
		}
	}
	gp := &goPackage{pkg: p.Types, fset: p.Fset, ops: map[string]*goOperation{}}
	if opErr := gp.findOperations(p.Syntax); opErr != nil {
		return nil, opErr
	}
	return gp, nil
}

// findOperations reads StrictServerInterface: each method is documented
// with "(METHOD /path)", which joins it to the spec without re-deriving
// oapi-codegen's names.
func (gp *goPackage) findOperations(files []*ast.File) error {
	obj := gp.pkg.Scope().Lookup("StrictServerInterface")
	if obj == nil {
		return errors.New("the package has no StrictServerInterface: generate it with strict-server: true")
	}
	iface, isIface := obj.Type().Underlying().(*types.Interface)
	if !isIface {
		return errors.New("StrictServerInterface is not an interface")
	}
	decl := interfaceDecl(files, "StrictServerInterface")
	if decl == nil {
		return errors.New("StrictServerInterface declaration not found")
	}
	for _, field := range decl.Methods.List {
		if len(field.Names) != 1 || field.Doc == nil {
			continue
		}
		name := field.Names[0].Name
		key := operationKey(field.Doc)
		if key == "" {
			return fmt.Errorf("StrictServerInterface.%s has no (METHOD /path) comment", name)
		}
		req, err := requestType(iface, name)
		if err != nil {
			return err
		}
		gp.ops[key] = &goOperation{Name: name, Request: req, Pos: field.Pos()}
		gp.order = append(gp.order, key)
	}
	return nil
}

func interfaceDecl(files []*ast.File, name string) *ast.InterfaceType {
	var decl *ast.InterfaceType
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if ts, isSpec := n.(*ast.TypeSpec); isSpec && ts.Name.Name == name {
				decl, _ = ts.Type.(*ast.InterfaceType)
			}
			return decl == nil
		})
	}
	return decl
}

// operationKey returns "METHOD /path" from a "// (METHOD /path)" line.
func operationKey(doc *ast.CommentGroup) string {
	for _, c := range doc.List {
		if m := opComment.FindStringSubmatch(strings.TrimPrefix(c.Text, "//")); m != nil {
			return m[1] + " " + m[2]
		}
	}
	return ""
}

// requestType returns the RequestObject type of an interface method.
func requestType(iface *types.Interface, name string) (*types.Named, error) {
	for fn := range iface.Methods() {
		if fn.Name() != name {
			continue
		}
		sig, _ := fn.Type().(*types.Signature)
		if sig == nil || sig.Params().Len() != 2 {
			return nil, fmt.Errorf("StrictServerInterface.%s: unexpected signature", name)
		}
		req, isNamed := types.Unalias(sig.Params().At(1).Type()).(*types.Named)
		if !isNamed {
			return nil, fmt.Errorf("StrictServerInterface.%s: request is not a named type", name)
		}
		return req, nil
	}
	return nil, fmt.Errorf("StrictServerInterface.%s: method not found", name)
}

// structOf returns the struct underlying t, if any.
func structOf(t types.Type) (*types.Struct, bool) {
	s, ok := types.Unalias(t).Underlying().(*types.Struct)
	return s, ok
}

// fieldByJSON finds the struct field whose json (or form) name is name.
func fieldByJSON(st *types.Struct, name string) (*types.Var, bool) {
	for i := range st.NumFields() {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))
		for _, key := range []string{"json", "form"} {
			if v, ok := tag.Lookup(key); ok {
				n, _, _ := strings.Cut(v, ",")
				if n == name {
					return f, true
				}
			}
		}
	}
	return nil, false
}
