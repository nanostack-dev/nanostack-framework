package genconfig

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen"
	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// missingType explains why the package has no type typeName: a syntax error,
// a declaration in a file that -tags=nanogen excludes, or no declaration.
func missingType(pkg *packages.Package, dir, typeName string) error {
	rel := func(f string) string { return relPath(dir, f) }
	var syntax []packages.Error
	for _, e := range pkg.Errors {
		if e.Kind == packages.ParseError {
			syntax = append(syntax, e)
		}
	}
	if len(syntax) > 0 {
		return errors.Join(gopkg.Problems(syntax, rel, "fix the syntax error")...)
	}
	fset := token.NewFileSet()
	for _, name := range pkg.IgnoredFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		if spec := typeSpec(f, typeName); spec != nil {
			at := fset.Position(spec.Pos())
			return gen.Errorf(rel(at.Filename), at.Line,
				"move "+typeName+" to a file without a build tag",
				"%s is in a file that -tags=%s excludes, and nano gen config loads with that tag", typeName, buildTag)
		}
	}
	return gen.Errorf("", 0, "pass -type with the root configuration struct",
		"package %s has no type %s", pkg.PkgPath, typeName)
}

// typeSpec returns the declaration of the type name in f, or nil.
func typeSpec(f *ast.File, name string) *ast.TypeSpec {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			if ts, isType := s.(*ast.TypeSpec); isType && ts.Name.Name == name {
				return ts
			}
		}
	}
	return nil
}
