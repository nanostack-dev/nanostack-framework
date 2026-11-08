package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	"github.com/nanostack-dev/nanostack-framework/gen"
	genapi "github.com/nanostack-dev/nanostack-framework/gen/api"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// Declaration paths, relative to the root.
const (
	projectDir  = "nanogen"
	projectFile = "nanogen/project.go"
	projectVar  = "Project"
)

// projectPkg is the import path of the declaration's types.
var projectPkg = reflect.TypeFor[project.Project]().PkgPath() //nolint:gochecknoglobals // a constant in effect.

// readProject reads nanogen/project.go under root. A root without the file
// has the zero Project, and declared is false. Values must be literals or
// constants: the file is type-checked, then its composite literal is read
// from the syntax tree.
func readProject(root string) (p project.Project, declared bool, err error) {
	if _, statErr := os.Stat(filepath.Join(root, projectFile)); errors.Is(statErr, os.ErrNotExist) {
		return project.Project{}, false, nil
	}
	p, err = readDeclaration(root)
	return p, err == nil, err
}

// readDeclaration reads the Project literal of nanogen/project.go.
func readDeclaration(root string) (project.Project, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
			packages.NeedFiles,
		Dir:  filepath.Join(root, projectDir),
		Fset: fset,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return project.Project{}, gopkg.LoadFailed(projectDir, err)
	}
	if len(pkgs) != 1 {
		return project.Project{}, gen.Errorf(projectFile, 0, "keep one package in "+projectDir,
			"%s holds %d packages", projectDir, len(pkgs))
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return project.Project{}, errors.Join(gopkg.CompileErrors(pkg, func(f string) string { return f },
			"make "+projectFile+" compile")...)
	}
	expr, err := projectLiteral(pkg)
	if err != nil {
		return project.Project{}, err
	}
	if err = onlyDeclared(fset, pkg); err != nil {
		return project.Project{}, err
	}
	d := &decoder{fset: fset, info: pkg.TypesInfo, lines: map[string]int{}}
	var p project.Project
	d.decode(expr, reflect.ValueOf(&p).Elem(), "Project")
	if len(d.problems) > 0 {
		return project.Project{}, errors.Join(d.problems...)
	}
	return p, checkProject(root, p, d)
}

// projectLiteral returns the value expression of var Project.
func projectLiteral(pkg *packages.Package) (ast.Expr, error) {
	obj, ok := pkg.Types.Scope().Lookup(projectVar).(*types.Var)
	if !ok || !isProjectType(obj.Type()) {
		return nil, gen.Errorf(projectFile, 0, "declare var Project = project.Project{...}",
			"%s declares no var Project of type project.Project", projectDir)
	}
	for _, f := range pkg.Syntax {
		if v := varValue(f, projectVar); v != nil {
			return v, nil
		}
	}
	return nil, gen.Errorf(projectFile, 0, "give var Project a value: var Project = project.Project{...}",
		"var Project has no value")
}

// onlyDeclared reports each use of var Project outside its declaration,
// such as an assignment in init: nano reads only the literal, so the value
// set there would be ignored.
func onlyDeclared(fset *token.FileSet, pkg *packages.Package) error {
	obj := pkg.Types.Scope().Lookup(projectVar)
	var errs []error
	for id, used := range pkg.TypesInfo.Uses {
		if used == obj {
			errs = append(errs, gen.Errorf(projectFile, fset.Position(id.Pos()).Line,
				"set every value in the var Project literal, and do not refer to Project elsewhere",
				"nano gen reads only the literal of var Project; this use of Project is ignored"))
		}
	}
	slices.SortFunc(errs, func(a, b error) int {
		var x, y *gen.Error
		errors.As(a, &x)
		errors.As(b, &y)
		return x.Line - y.Line
	})
	return errors.Join(errs...)
}

func isProjectType(t types.Type) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == projectPkg && named.Obj().Name() == "Project"
}

// varValue returns the value of the package-level var name in f, or nil.
func varValue(f *ast.File, name string) ast.Expr {
	for _, decl := range f.Decls {
		gd, isGen := decl.(*ast.GenDecl)
		if !isGen || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for i, id := range vs.Names {
				if id.Name == name && i < len(vs.Values) {
					return vs.Values[i]
				}
			}
		}
	}
	return nil
}

// decoder reads a composite literal into a value of the project types.
type decoder struct {
	fset     *token.FileSet
	info     *types.Info
	problems []error
	lines    map[string]int // field path, such as "API.Spec" -> its line
}

func (d *decoder) line(n ast.Node) int { return d.fset.Position(n.Pos()).Line }

// decode sets v from expr. path names the value in messages.
func (d *decoder) decode(expr ast.Expr, v reflect.Value, path string) {
	switch v.Kind() {
	case reflect.Pointer:
		if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
			return
		}
		u, ok := expr.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			d.notLiteral(expr, path, "&project."+v.Type().Elem().Name()+"{...} literal")
			return
		}
		v.Set(reflect.New(v.Type().Elem()))
		d.decode(u.X, v.Elem(), path)
	case reflect.Struct:
		lit, ok := expr.(*ast.CompositeLit)
		if !ok {
			d.notLiteral(expr, path, "project."+v.Type().Name()+"{...} literal")
			return
		}
		d.decodeStruct(lit, v, path)
	case reflect.Slice:
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
	case reflect.String:
		c := d.constant(expr)
		if c == nil || c.Kind() != constant.String {
			d.notLiteral(expr, path, "string")
			return
		}
		v.SetString(constant.StringVal(c))
	case reflect.Bool:
		c := d.constant(expr)
		if c == nil || c.Kind() != constant.Bool {
			d.notLiteral(expr, path, "bool")
			return
		}
		v.SetBool(constant.BoolVal(c))
	default:
		panic("nano gen: project field kind " + v.Kind().String() + " has no decoder")
	}
}

func (d *decoder) decodeStruct(lit *ast.CompositeLit, v reflect.Value, path string) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			d.problems = append(d.problems, gen.Errorf(projectFile, d.line(elt), "name each field: Field: value",
				"%s has a value without a field name", path))
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue // the type check reported it
		}
		f := v.FieldByName(key.Name)
		if !f.IsValid() {
			continue // the type check reported it
		}
		sub := strings.TrimPrefix(path+"."+key.Name, "Project.")
		d.lines[sub] = d.line(kv)
		d.decode(kv.Value, f, sub)
	}
}

// constant returns the constant value of expr: a literal, a named constant
// or a constant expression. nil when expr is not constant.
func (d *decoder) constant(expr ast.Expr) constant.Value {
	if tv, ok := d.info.Types[expr]; ok {
		return tv.Value
	}
	return nil
}

func (d *decoder) notLiteral(expr ast.Expr, path, want string) {
	var b bytes.Buffer
	if err := format.Node(&b, d.fset, expr); err != nil {
		b.WriteString("the value")
	}
	d.problems = append(d.problems, gen.Errorf(projectFile, d.line(expr),
		"replace "+b.String()+" with a "+want,
		"project values must be literals or constants; %s is %s", path, b.String()))
}

// checkProject reports the declared paths that do not exist, and the
// outputs that would leave the directory nano gen runs on.
func checkProject(root string, p project.Project, d *decoder) error {
	c := &projectCheck{root: root, d: d}
	if p.API != nil {
		c.api(p.API)
	}
	if p.Env != "" {
		c.relative("Env", p.Env)
	}
	return errors.Join(c.errs...)
}

// projectCheck collects the problems of one declaration.
type projectCheck struct {
	root string
	d    *decoder
	errs []error
}

// at returns the line of the first of fields that the declaration sets.
func (c *projectCheck) at(fields ...string) int {
	for _, f := range fields {
		if line := c.d.lines[f]; line > 0 {
			return line
		}
	}
	return 0
}

func (c *projectCheck) add(line int, fix, format string, args ...any) {
	c.errs = append(c.errs, gen.Errorf(projectFile, line, fix, format, args...))
}

func (c *projectCheck) relative(field, path string) bool {
	if filepath.IsAbs(path) {
		c.add(c.at(field), "write the path relative to the directory nano gen runs on", "%s %s is absolute",
			field, path)
		return false
	}
	return true
}

// inside reports an output directory outside the directory nano gen runs
// on (design P10). An input, such as the spec, may sit outside it.
func (c *projectCheck) inside(field, path string) bool {
	if outside(path) {
		c.add(c.at(field, "API.Clients"),
			"generate under the directory nano gen runs on; a module beside it declares its own nanogen/project.go",
			"%s %s is outside the directory nano gen runs on", field, path)
		return false
	}
	return true
}

func (c *projectCheck) fileName(field, name string) {
	if name != "" && (name != filepath.Base(name) || !strings.HasSuffix(name, ".go")) {
		c.add(c.at(field, "API.Clients"), "set "+field+" to a file name such as api.gen.go",
			"%s %s is not a .go file name", field, name)
	}
}

func (c *projectCheck) api(api *project.OpenAPI) {
	switch {
	case api.Spec == "":
		c.add(c.at("API"), "set API.Spec to the OpenAPI document", "API.Spec is empty")
	case c.relative("API.Spec", api.Spec):
		if fi, err := os.Stat(filepath.Join(c.root, api.Spec)); err != nil || fi.IsDir() {
			c.add(c.at("API.Spec"), specFix(c.root), "API.Spec %s does not exist", api.Spec)
		}
	}
	outputs := map[string]string{} // output path -> the field that writes it
	switch dir := api.Server.Dir; {
	case dir != "":
		if c.relative("API.Server.Dir", dir) && c.inside("API.Server.Dir", dir) &&
			!dirExists(filepath.Join(c.root, dir)) {
			c.add(c.at("API.Server.Dir"), "set API.Server.Dir to the directory of the oapi-codegen package",
				"API.Server.Dir %s is not a directory", dir)
		}
		outputs[filepath.Join(dir, orDefault(api.Server.File, genapi.DefaultServerFile))] = "API.Server"
	case len(api.Clients) == 0:
		c.add(c.at("API"), "set API.Server.Dir to the server package, or add a client to API.Clients",
			"API declares neither a server nor a client")
	}
	c.fileName("API.Server.File", api.Server.File)
	for i, cl := range api.Clients {
		c.client(fmt.Sprintf("API.Clients[%d]", i), cl, outputs)
	}
}

// client checks one client; outputs holds the outputs of the server and the
// clients before it.
func (c *projectCheck) client(field string, cl project.Client, outputs map[string]string) {
	switch {
	case cl.Dir == "":
		c.add(c.at(field+".Package", "API.Clients"), "set "+field+".Dir to the client package directory",
			"%s.Dir is empty", field)
	case c.relative(field+".Dir", cl.Dir):
		c.inside(field+".Dir", cl.Dir)
	}
	if !token.IsIdentifier(cl.Package) {
		c.add(c.at(field+".Package", field+".Dir", "API.Clients"), "set "+field+".Package to a Go package name",
			"%s.Package %q is not a Go package name", field, cl.Package)
	}
	c.fileName(field+".File", cl.File)
	out := filepath.Join(cl.Dir, orDefault(cl.File, genapi.DefaultClientFile))
	if other, dup := outputs[out]; dup {
		c.add(c.at(field+".Dir", field+".Package", "API.Clients"), "give "+field+" its own Dir or File",
			"%s and %s both write %s", other, field, filepath.ToSlash(out))
	}
	outputs[out] = field
}

// outside reports whether the relative path leaves the directory it is
// relative to.
func outside(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// specFix names the OpenAPI documents under root, when there are some.
func specFix(root string) string {
	specs := findSpecs(root)
	if len(specs) == 0 {
		return "write the path of the OpenAPI document, relative to the directory nano gen runs on"
	}
	return "the module has " + strings.Join(specs, ", ")
}

// findSpecs returns the openapi.yaml, .yml and .json files under root,
// relative to it, with the walk rules of discovery.
func findSpecs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a directory it cannot read holds no suggestion
		}
		if e.IsDir() {
			if path != root && skipDir(path, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if slices.Contains([]string{"openapi.yaml", "openapi.yml", "openapi.json"}, e.Name()) {
			rel, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}
