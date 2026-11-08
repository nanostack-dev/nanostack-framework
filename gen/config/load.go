package genconfig

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

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

const (
	runtimePath = "github.com/nanostack-dev/nanostack-framework/core/config"
	secretPath  = "github.com/nanostack-dev/nanostack-framework/core/secret"
	tagConfig   = "config"
	tagEnv      = "env"
	tagDefault  = "default"
	tagValidate = "validate"
)

var (
	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// textUnmarshaler is encoding.TextUnmarshaler, built here so the check needs
// no loaded "encoding" package.
var textUnmarshaler = func() *types.Interface {
	errType := types.Universe.Lookup("error").Type()
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "text", types.NewSlice(types.Typ[types.Byte]))),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", errType)), false)
	m := types.NewFunc(token.NoPos, nil, "UnmarshalText", sig)
	return types.NewInterfaceType([]*types.Func{m}, nil).Complete()
}()

// loader walks a root struct type into a model.
type loader struct {
	dir      string
	fset     *token.FileSet
	model    *model
	problems []string
	docs     map[string]*ast.File // root package syntax, by absolute path
	ext      map[string]*ast.File // other packages' files, parsed on demand
	extFset  *token.FileSet
	envs     map[string]string // env name -> setting path, to catch clashes
	paths    map[string]bool
}

func load(dir, typeName, out string) (*model, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	pkgName, err := gopkg.PackageName(absDir, out)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedFiles,
		Dir:  absDir,
		Fset: fset,
		// Replace the previous output with stubs of the generated API, so a
		// stale or broken output never blocks regeneration while hand-written
		// code in the package can still call Bind or String.
		Overlay: map[string][]byte{filepath.Join(absDir, out): stubSource(pkgName, typeName)},
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("load package %s: %w", dir, err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("%s: expected one package, found %d", dir, len(pkgs))
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		msgs := make([]string, len(pkg.Errors))
		for i, e := range pkg.Errors {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("package %s does not compile; fix it first:\n%s", pkg.PkgPath, strings.Join(msgs, "\n"))
	}
	obj, ok := pkg.Types.Scope().Lookup(typeName).(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s: no type %s; pass -type with the root configuration struct", pkg.PkgPath, typeName)
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a struct type", position(fset, absDir, obj.Pos()), typeName)
	}
	l := &loader{
		dir:     absDir,
		fset:    fset,
		docs:    map[string]*ast.File{},
		ext:     map[string]*ast.File{},
		extFset: token.NewFileSet(),
		envs:    map[string]string{},
		paths:   map[string]bool{},
		model: &model{
			pkgPath:  pkg.PkgPath,
			pkgName:  pkg.Name,
			typeName: typeName,
			source:   filepath.Base(fset.Position(obj.Pos()).Filename),
		},
	}
	for _, f := range pkg.Syntax {
		l.docs[fset.Position(f.Package).Filename] = f
	}
	l.model.root = l.walk(st, scope{}, map[*types.Struct]bool{})
	if len(l.problems) > 0 {
		return nil, errors.New(strings.Join(l.problems, "\n"))
	}
	if len(l.model.settings) == 0 {
		return nil, fmt.Errorf(
			"%s: %s has no settings; tag fields with config:\"name\"",
			position(fset, absDir, obj.Pos()),
			typeName,
		)
	}
	return l.model, nil
}

// stubSource declares the generated API with panicking bodies. It is only
// ever type-checked, never compiled into a binary.
func stubSource(pkgName, typeName string) []byte {
	bind := bindFuncName(typeName)
	return []byte("package " + pkgName + `

import (
	"context"
	"log/slog"

	nanoconf "` + runtimePath + `"
)

func ` + bind + `(context.Context, *nanoconf.Sources) (` + typeName + `, error) { panic("nano gen config stub") }
func (` + typeName + `) LogValue() slog.Value { panic("nano gen config stub") }
func (` + typeName + `) String() string { panic("nano gen config stub") }
func (` + typeName + `) GoString() string { panic("nano gen config stub") }
`)
}

func (l *loader) problemf(at, format string, args ...any) {
	l.problems = append(l.problems, at+": "+fmt.Sprintf(format, args...))
}

// scope is the position of one struct level in the configuration tree.
type scope struct {
	path  string // dotted setting path prefix
	env   string // environment variable prefix
	goSel string // Go selector prefix, appended to "c."
}

// fieldTag is a parsed config tag.
type fieldTag struct {
	name     string
	optional bool
	path     string
	env      string
}

// walk visits one struct level and returns its group of settings.
func (l *loader) walk(st *types.Struct, sc scope, seen map[*types.Struct]bool) *group {
	g := &group{}
	if seen[st] {
		return g
	}
	seen[st] = true
	defer delete(seen, st)
	for i := range st.NumFields() {
		g.children = append(g.children, l.field(st.Field(i), reflect.StructTag(st.Tag(i)), sc, seen)...)
	}
	return g
}

// field turns one struct field into nodes: none (skipped or invalid), the
// inlined children of an embedded struct, a nested group, or a setting.
func (l *loader) field(f *types.Var, tag reflect.StructTag, sc scope, seen map[*types.Struct]bool) []node {
	at := position(l.fset, l.dir, f.Pos())
	goSel := joinNonEmpty(".", sc.goSel, f.Name())
	cfgTag, hasTag := tag.Lookup(tagConfig)
	switch {
	case cfgTag == "-":
		return nil
	case !f.Exported():
		if hasTag {
			l.problemf(at, "%s: unexported fields cannot be bound; export it or drop the config tag", f.Name())
		}
		return nil
	case !hasTag:
		if inner, ok := f.Type().Underlying().(*types.Struct); ok && f.Embedded() {
			return l.walk(inner, scope{path: sc.path, env: sc.env, goSel: goSel}, seen).children
		}
		l.problemf(at, "%s: add a config:\"name\" tag, or config:\"-\" to skip the field", f.Name())
		return nil
	}
	ft, ok := l.parseTag(f, tag, cfgTag, sc, at)
	if !ok {
		return nil
	}
	if inner, isStruct := nestedStruct(f.Type()); isStruct {
		return l.nested(f, tag, ft, inner, goSel, at, seen)
	}
	s := &setting{
		path: ft.path, env: ft.env, name: ft.name, goExpr: goSel, typ: f.Type(),
		optional: ft.optional, doc: l.fieldDoc(f), at: at,
		def: tag.Get(tagDefault), rawCheck: tag.Get(tagValidate),
	}
	if !l.leaf(f, tag, s) {
		return nil
	}
	l.model.settings = append(l.model.settings, s)
	return []node{{setting: s}}
}

// parseTag reads the config and env tags and derives the path and env name.
func (l *loader) parseTag(f *types.Var, tag reflect.StructTag, cfgTag string, sc scope, at string) (fieldTag, bool) {
	name, opts, _ := strings.Cut(cfgTag, ",")
	ft := fieldTag{
		name: name,
		path: joinNonEmpty(".", sc.path, name),
		env:  joinNonEmpty("_", sc.env, strings.ToUpper(name)),
	}
	switch {
	case !keyPattern.MatchString(name):
		l.problemf(at, "%s: config name %q must be lower snake case, for example config:\"due_days\"", f.Name(), name)
		return ft, false
	case l.paths[ft.path]:
		l.problemf(at, "%s: setting %s is declared twice", f.Name(), ft.path)
		return ft, false
	case opts != "" && opts != "optional":
		l.problemf(at, "%s: unknown config tag option %q; the only option is optional", f.Name(), opts)
		return ft, false
	}
	ft.optional = opts == "optional"
	if e, set := tag.Lookup(tagEnv); set {
		if !envPattern.MatchString(e) {
			l.problemf(at, "%s: env %q must be upper snake case, for example env:\"BILLING_FROM\"", f.Name(), e)
			return ft, false
		}
		ft.env = e
	}
	return ft, true
}

func (l *loader) nested(
	f *types.Var,
	tag reflect.StructTag,
	ft fieldTag,
	inner *types.Struct,
	goSel, at string,
	seen map[*types.Struct]bool,
) []node {
	for _, t := range []string{tagDefault, tagValidate} {
		if _, set := tag.Lookup(t); set {
			l.problemf(at, "%s: %s does not apply to a nested struct; set it on its fields", f.Name(), t)
		}
	}
	if ft.optional {
		l.problemf(at, "%s: optional does not apply to a nested struct; mark its fields optional", f.Name())
	}
	l.paths[ft.path] = true
	sub := l.walk(inner, scope{path: ft.path, env: ft.env, goSel: goSel}, seen)
	sub.name = ft.name
	return []node{{group: sub}}
}

// leaf classifies and validates one setting, recording any problem.
func (l *loader) leaf(f *types.Var, tag reflect.StructTag, s *setting) bool {
	if !l.classify(s) {
		return false
	}
	if prev, clash := l.envs[s.env]; clash {
		l.problemf(s.at, "%s: env %s is already used by %s; set a distinct env tag", f.Name(), s.env, prev)
		return false
	}
	l.envs[s.env] = s.path
	l.paths[s.path] = true
	_, hasDefault := tag.Lookup(tagDefault)
	switch {
	case hasDefault && s.def == "":
		l.problemf(
			s.at,
			"%s: default:\"\" is not a default; use config:\"%s,optional\" for a setting that may stay empty",
			f.Name(),
			s.name,
		)
	case s.def != "" && s.optional:
		l.problemf(s.at, "%s: a setting with a default is never empty; drop ,optional", f.Name())
	case s.def != "" && s.secret():
		l.problemf(s.at, "%s: secrets cannot have a default, it would put the secret in source code", f.Name())
	default:
		if err := validateSetting(s); err != nil {
			l.problemf(s.at, "%s: %v", f.Name(), err)
			return false
		}
		return true
	}
	return false
}

// nestedStruct reports whether t is a struct to descend into, rather than a
// value the parse helpers handle.
func nestedStruct(t types.Type) (*types.Struct, bool) {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	if _, isText := scalarKind(t); isText {
		return nil, false
	}
	return st, true
}

// classify sets the kind of s, or records why its type is unsupported.
func (l *loader) classify(s *setting) bool {
	t := s.typ
	if sl, ok := t.(*types.Slice); ok {
		s.list = true
		t = sl.Elem()
	}
	k, ok := scalarKind(t)
	if !ok {
		l.problemf(
			s.at,
			"%s: type %s is not supported; use a string, bool, number, time.Duration, *url.URL, secret.Secret, a type implementing encoding.TextUnmarshaler, a slice of those, or a nested struct",
			lastSegment(s.goExpr),
			types.TypeString(s.typ, packageNameQualifier),
		)
		return false
	}
	s.kind, s.elemType = k, t
	return true
}

// scalarKind classifies a single-value type.
func scalarKind(t types.Type) (kind, bool) {
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() != nil {
			switch obj.Pkg().Path() + "." + obj.Name() {
			case "time.Duration":
				return kindDuration, true
			case secretPath + ".Secret":
				return kindSecret, true
			}
		}
	}
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		if named, ok := ptr.Elem().(*types.Named); ok && named.Obj().Pkg() != nil &&
			named.Obj().Pkg().Path() == "net/url" && named.Obj().Name() == "URL" {
			return kindURL, true
		}
		return 0, false
	}
	if _, isNamed := t.(*types.Named); isNamed && types.Implements(types.NewPointer(t), textUnmarshaler) {
		return kindText, true
	}
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return 0, false
	}
	info := basic.Info()
	switch {
	case info&types.IsString != 0:
		return kindString, true
	case info&types.IsBoolean != 0:
		return kindBool, true
	case info&types.IsInteger != 0 && info&types.IsUnsigned != 0 && basic.Kind() != types.Uintptr:
		return kindUint, true
	case info&types.IsInteger != 0 && info&types.IsUnsigned == 0:
		return kindInt, true
	case info&types.IsFloat != 0:
		return kindFloat, true
	}
	return 0, false
}

func position(fset *token.FileSet, dir string, p token.Pos) string {
	pos := fset.Position(p)
	name := pos.Filename
	if rel, err := filepath.Rel(dir, name); err == nil && !strings.HasPrefix(rel, "..") {
		name = rel
	}
	return fmt.Sprintf("%s:%d", name, pos.Line)
}

func joinNonEmpty(sep, a, b string) string {
	if a == "" {
		return b
	}
	return a + sep + b
}

func lastSegment(s string) string { return s[strings.LastIndex(s, ".")+1:] }

func packageNameQualifier(p *types.Package) string { return p.Name() }
