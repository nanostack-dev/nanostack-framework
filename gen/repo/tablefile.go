package genrepo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gopkg"
)

// tableNames are the names a table file gives a table and its columns.
type tableNames struct {
	schema  string
	table   string
	columns map[string]string // field -> column name
}

// tableFiles is what the files of one table package declare.
type tableFiles struct {
	tables      map[string]tableNames        // table variable -> names
	keys        map[string][]uniqueKey       // table type, such as ItemsTable -> its UniqueKeys
	types       map[string]map[string]string // table type -> its ColumnTypes: column name -> catalogue type
	foreignKeys map[string][]foreignKey      // table type -> its ForeignKeys
}

// tableSource reads what the table package at path declares.
func (l *loaded) tableSource(path string) (*tableFiles, error) {
	files, err := l.parsed(path)
	if err != nil {
		return nil, err
	}
	src := &tableFiles{tables: map[string]tableNames{}, keys: map[string][]uniqueKey{},
		types: map[string]map[string]string{}, foreignKeys: map[string][]foreignKey{}}
	for _, f := range files {
		readTableFile(f, src)
		readUniqueKeys(l.fset, f, src)
		readColumnTypes(f, src)
		readForeignKeys(f, src)
	}
	return src, nil
}

// parsed returns the parsed Go files of the package at path, with the
// overlay, by parsing alone: the names nanostack gen db gives tables, columns,
// enums and unique keys are string literals, comments and file names in
// them. A file that does
// not parse is left out; the type check reports it.
func (l *loaded) parsed(path string) (map[string]*ast.File, error) {
	if files, ok := l.files[path]; ok {
		return files, nil
	}
	overlay, err := gopkg.Overlay(l.overlay, nil)
	if err != nil {
		return nil, err
	}
	pkgs, err := gopkg.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles, Dir: l.dir,
		Overlay: overlay}, path)
	if err != nil {
		return nil, gopkg.LoadFailed(l.dir, err)
	}
	files := map[string]*ast.File{}
	if l.fset == nil {
		l.fset = token.NewFileSet()
	}
	for _, p := range pkgs {
		for _, name := range p.GoFiles {
			data, ok := overlay[name]
			if !ok {
				if data, err = os.ReadFile(filepath.Clean(name)); err != nil {
					continue
				}
			}
			f, parseErr := parser.ParseFile(l.fset, name, data, parser.SkipObjectResolution|parser.ParseComments)
			if parseErr == nil {
				files[name] = f
			}
		}
	}
	if l.files == nil {
		l.files = map[string]map[string]*ast.File{}
	}
	l.files[path] = files
	return files, nil
}

// enumType returns the schema-qualified, quoted name of the PostgreSQL enum
// that the model type n stands for: nanostack gen db writes the type of enum e
// into the model file e.go. ok is false when no such file declares n.
func (l *loaded) enumType(t types.Type, schema string) (string, bool) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return "", false
	}
	files, err := l.parsed(n.Obj().Pkg().Path())
	if err != nil {
		return "", false
	}
	for name, f := range files {
		if declaresType(f, n.Obj().Name()) {
			enum := strings.TrimSuffix(filepath.Base(name), ".go")
			return strconv.Quote(schema) + "." + strconv.Quote(enum), true
		}
	}
	return "", false
}

// declaresType reports whether f declares the type name.
func declaresType(f *ast.File, name string) bool {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, isType := spec.(*ast.TypeSpec); isType && ts.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// readTableFile reads the table variables of f, var X = newXTable("schema",
// "x", ""), and the column names of newXTableImpl.
func readTableFile(f *ast.File, src *tableFiles) {
	constructors := map[string]string{} // newXTable -> variable
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			for _, spec := range gd.Specs {
				readTableVar(spec, src, constructors)
			}
		}
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Body == nil || !strings.HasSuffix(fd.Name.Name, "Impl") {
			continue
		}
		if variable, known := constructors[strings.TrimSuffix(fd.Name.Name, "Impl")]; known {
			readColumns(fd.Body, src.tables[variable].columns)
		}
	}
}

// readColumnTypes reads each func (XTable) ColumnTypes() map[string]string
// of f: the catalogue type of each column, by column name, from the
// "column": "type" entries of the returned literal.
func readColumnTypes(f *ast.File, src *tableFiles) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "ColumnTypes" || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Body == nil {
			continue
		}
		recv := identOf(fd.Recv.List[0].Type)
		if recv == nil {
			continue
		}
		types := map[string]string{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			kv, isKV := n.(*ast.KeyValueExpr)
			if !isKV {
				return true
			}
			col, isCol := stringLit(kv.Key)
			typ, isType := stringLit(kv.Value)
			if isCol && isType {
				types[col] = typ
			}
			return false
		})
		src.types[recv.Name] = types
	}
}

// readTableVar reads one var X = newXTable("schema", "x", "") spec.
func readTableVar(spec ast.Spec, src *tableFiles, constructors map[string]string) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
		return
	}
	call, ok := vs.Values[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 3 {
		return
	}
	fn := identOf(call.Fun)
	schema, isSchema := stringLit(call.Args[0])
	name, isName := stringLit(call.Args[1])
	if fn == nil || !isSchema || !isName {
		return
	}
	constructors[fn.Name] = vs.Names[0].Name
	src.tables[vs.Names[0].Name] = tableNames{schema: schema, table: name, columns: map[string]string{}}
}

// readColumns reads `XColumn = postgres.StringColumn("x")` and the `X:
// XColumn` fields of the returned literal.
func readColumns(body *ast.BlockStmt, out map[string]string) {
	byVar := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			if variable, name, ok := columnVar(x); ok {
				byVar[variable] = name
			}
		case *ast.KeyValueExpr:
			key, value := identOf(x.Key), identOf(x.Value)
			if key == nil || value == nil {
				break
			}
			if name, ok := byVar[value.Name]; ok {
				out[key.Name] = name
			}
		}
		return true
	})
}

// columnVar reads XColumn = postgres.StringColumn("x").
func columnVar(vs *ast.ValueSpec) (variable, name string, ok bool) {
	if len(vs.Names) != 1 || len(vs.Values) != 1 {
		return "", "", false
	}
	call, isCall := vs.Values[0].(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return "", "", false
	}
	name, ok = stringLit(call.Args[0])
	return vs.Names[0].Name, name, ok
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
