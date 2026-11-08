package genvalidate

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
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

// loadPackage type-checks the package in dir with the generated output
// file replaced by an empty one, so a stale or missing validate.gen.go never
// blocks generation. Type errors are expected (the strict template calls
// methods this generator writes) and are ignored; the types are still
// complete.
func loadPackage(opts Options) (*goPackage, error) {
	dir := opts.Dir
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, gen.EnvErrorf("run from a directory that exists", "resolve %s: %v", dir, err)
	}
	absOut := filepath.Join(absDir, filepath.Base(opts.Output))
	name, err := gopkg.PackageName(absDir, filepath.Base(absOut))
	if err != nil {
		return nil, err
	}
	overlay, err := gopkg.Overlay(opts.Overlay, map[string][]byte{absOut: []byte("package " + name + "\n")})
	if err != nil {
		return nil, err
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
		return nil, gopkg.LoadFailed(dir, err)
	}
	if len(pkgs) != 1 || pkgs[0].Types == nil {
		return nil, gen.Errorf(dir, 0, "pass -dir with the directory of the oapi-codegen package",
			"not one Go package")
	}
	p := pkgs[0]
	// Type errors, including the compile errors go list reports, are
	// expected: the strict template calls the methods generated here.
	var syntax []packages.Error
	for _, e := range p.Errors {
		if e.Kind == packages.ParseError {
			syntax = append(syntax, e)
		}
	}
	if len(syntax) > 0 {
		return nil, errors.Join(gopkg.Problems(syntax, relWD, "fix the syntax error")...)
	}
	gp := &goPackage{pkg: p.Types, fset: p.Fset, ops: map[string]*goOperation{}}
	if opErr := gp.findOperations(dir, p.Syntax); opErr != nil {
		return nil, opErr
	}
	return gp, nil
}

// strictFix is the fix when the package lacks what oapi-codegen's strict
// server writes.
const strictFix = "generate the package with oapi-codegen and strict-server: true; " +
	"nano gen validate reads the StrictServerInterface it writes"

// findOperations reads StrictServerInterface: each method is documented
// with "(METHOD /path)", which joins it to the spec without re-deriving
// oapi-codegen's names.
func (gp *goPackage) findOperations(dir string, files []*ast.File) error {
	obj := gp.pkg.Scope().Lookup("StrictServerInterface")
	if obj == nil {
		return gen.Errorf(dir, 0, strictFix, "the package has no StrictServerInterface")
	}
	at := gp.fset.Position(obj.Pos())
	iface, isIface := obj.Type().Underlying().(*types.Interface)
	if !isIface {
		return gen.Errorf(relWD(at.Filename), at.Line, strictFix, "StrictServerInterface is not an interface")
	}
	decl := interfaceDecl(files, "StrictServerInterface")
	if decl == nil {
		return gen.Errorf(relWD(at.Filename), at.Line, strictFix, "StrictServerInterface declaration not found")
	}
	for _, field := range decl.Methods.List {
		if len(field.Names) != 1 || field.Doc == nil {
			continue
		}
		name := field.Names[0].Name
		key := operationKey(field.Doc)
		fieldAt := gp.fset.Position(field.Pos())
		if key == "" {
			return gen.Errorf(relWD(fieldAt.Filename), fieldAt.Line, strictFix,
				"StrictServerInterface.%s has no (METHOD /path) comment", name)
		}
		req, msg := requestType(iface, name)
		if msg != "" {
			return gen.Errorf(relWD(fieldAt.Filename), fieldAt.Line, strictFix, "%s", msg)
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

// requestType returns the RequestObject type of an interface method, or
// what is wrong with the method.
func requestType(iface *types.Interface, name string) (*types.Named, string) {
	for fn := range iface.Methods() {
		if fn.Name() != name {
			continue
		}
		sig, _ := fn.Type().(*types.Signature)
		if sig == nil || sig.Params().Len() != 2 {
			return nil, fmt.Sprintf("StrictServerInterface.%s: unexpected signature", name)
		}
		req, isNamed := types.Unalias(sig.Params().At(1).Type()).(*types.Named)
		if !isNamed {
			return nil, fmt.Sprintf("StrictServerInterface.%s: request is not a named type", name)
		}
		return req, ""
	}
	return nil, fmt.Sprintf("StrictServerInterface.%s: method not found", name)
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
