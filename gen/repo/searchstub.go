package genrepo

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

// stubSearches are the searches of a package, read as a run reads them,
// for its forward stubs.
type stubSearches struct {
	l      *loaded
	byFile map[string][]*searchDecl // absolute path of the declaring file -> its searches
	all    []*searchDecl
}

// mappers returns the stub of each API mapper that goes into another
// package, next to its handler, by absolute path.
func (t *stubSearches) mappers() map[string][]byte {
	if t == nil {
		return nil
	}
	files, err := t.l.renderMappers(t.all, true)
	if err != nil {
		return nil
	}
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.Path] = f.Data
	}
	return out
}

// stubCache keeps the searches of each package between the calls of one
// pipeline run, which asks for the stubs of a package once per earlier
// unit. An entry holds while the files of the package, of the table
// packages it reads, and of their model and enum packages have not changed.
var stubCache = struct { //nolint:gochecknoglobals // a cache of a pure function of the files
	sync.Mutex
	byDir map[string]stubEntry
}{byDir: map[string]stubEntry{}}

type stubEntry struct {
	stamp  string
	dirs   []string
	result *stubSearches
}

// typedSearches loads the package in dir as a run does, with the files nanostack
// gen repo wrote left empty, and reads its declarations. It returns nil when
// the package does not load or a declaration has a problem. A nil result is
// not cached: a table package that the declarations need can appear later
// in the same pipeline run, when the db step writes it, and no stamped
// directory tells of that.
func typedSearches(dir string) *stubSearches {
	stubCache.Lock()
	entry, cached := stubCache.byDir[dir]
	stubCache.Unlock()
	if cached && entry.stamp == stamp(entry.dirs) {
		return entry.result
	}
	result, dirs := readSearches(dir)
	if result == nil {
		return nil
	}
	stubCache.Lock()
	stubCache.byDir[dir] = stubEntry{stamp: stamp(dirs), dirs: dirs, result: result}
	stubCache.Unlock()
	return result
}

// readSearches reads the searches of the package in dir, and returns the
// directories whose files they depend on.
func readSearches(dir string) (*stubSearches, []string) {
	dirs := []string{dir}
	l, err := loadPackage(Options{Dir: dir})
	if err != nil {
		return nil, dirs
	}
	l.stub = true
	decls, err := l.declarations()
	for path := range l.files {
		for name := range l.files[path] {
			// A table package of nanostack gen db sits next to its model and enum
			// packages, which a builder reads too.
			tableDir := filepath.Dir(name)
			dirs = append(dirs, tableDir, filepath.Join(filepath.Dir(tableDir), "model"),
				filepath.Join(filepath.Dir(tableDir), "enum"))
		}
	}
	slices.Sort(dirs)
	dirs = slices.Compact(dirs)
	if err != nil {
		return nil, dirs
	}
	out := &stubSearches{l: l, byFile: map[string][]*searchDecl{}, all: decls.searches}
	for _, s := range decls.searches {
		path := filepath.Join(l.dir, s.at.file)
		out.byFile[path] = append(out.byFile[path], s)
	}
	return out, dirs
}

// stamp names the state of the Go files of dirs: their names, sizes and
// times of change.
func stamp(dirs []string) string {
	var b strings.Builder
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			b.WriteString(dir + ":missing\n")
			continue
		}
		for _, e := range entries {
			info, infoErr := e.Info()
			if infoErr != nil || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			fmt.Fprintf(&b, "%s/%s:%d:%d\n", dir, e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// of returns the searches declared in the file at path.
func (t *stubSearches) of(path string) []*searchDecl {
	if t == nil {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	return t.byFile[abs]
}

// stub renders a forward stub with the methods of the parsed stub, whose
// imports are imports (name -> path), and the builders of searches. ok is
// false when an import name of the methods is not free for the builders.
func (t *stubSearches) stub(pkg string, imports map[string]string, methods []byte, searches []*searchDecl) (
	[]byte, bool,
) {
	e := &emitter{imp: importset.New(t.l.pkg.PkgPath), self: t.l.pkg.PkgPath}
	e.imp.Block(t.l.pkg.Types.Scope().Names()...)
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(imports[a], imports[b]) })
	for _, name := range names {
		if e.imp.Add(imports[name], name) != name {
			return nil, false
		}
	}
	e.b.Write(methods)
	for _, s := range searches {
		if s.builder == nil {
			continue
		}
		if len(s.unbuilt) > 0 {
			e.scopeStruct(s.builder)
			continue
		}
		e.searchBuilder(s.builder)
		if s.apiMapper != nil && s.apiMapper.home == nil {
			e.searchAPI(s.builder, func(ident string) string { return ident }, true)
		}
	}
	src, err := e.file(pkg, "the forward stub")
	return src, err == nil
}
