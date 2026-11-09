// Package genrepo implements nanostack gen repo: it reads the repository
// declarations of one package and writes repo_base.gen.go, one generated base
// per table they name, and next to each declaring file <file>_repo.gen.go,
// with the methods of the bound interfaces that derive from their names.
// ADR 0026 records the design.
//
// A declaration is a call to package db/nanorepo inside func _() { ... }, in
// a file with no build tag:
//
//	func _() {
//		nanorepo.Implement[ProductUserRepository, productUserRepositoryImpl](table.ProductUsers, productUserToDomain)
//		nanorepo.Base[model.AuditEvents](table.AuditEvents)
//	}
//
// The base of table product_users is the struct productUsersBase. The
// implementation embeds it and sets it with newProductUsersBase(db). It
// holds:
//
//   - t, the table, and where, typed predicates on its columns:
//     where.Email(v), EmailIn(vs...), EmailContains(term)
//     (searchexec.Contains: a substring without case, on a column whose
//     ColumnTypes type is text), EmailEqualsIgnoreCase(v), CreatedAtAfter(v),
//     CreatedAtBefore(v), ExternalIDIsNull();
//   - helpers on a condition, which map rows with the declaration's mapper:
//     findOne (LIMIT 2: a second row is transactor.ErrMoreThanOneRow),
//     first (needs an order), findAll, exists, count, deleteWhere and
//     updateWhere (they return the affected rows and refuse a nil
//     condition);
//   - the pkg/db/transactor functions as methods, generic where the
//     function is (Go 1.27): query, queryOptional, queryMap,
//     queryOptionalMap, queryMapSlice, queryCount, exec, execAffected, page,
//     and queryRow, queryRowOptional and queryRows, which map with the
//     mapper.
//
// Every helper runs through pkg/db/transactor, so the context transaction
// and the row-lock rules apply: findOne, first and findAll honour
// transactor.ForUpdate; PostgreSQL refuses a lock on exists and count, which
// aggregate; deleteWhere and updateWhere return ErrRowLockNotSelect or
// ErrRowLockOutsideTx.
//
// The mapper is a function, or a variable of the declaring package set to a
// function or to a method value on a type without fields: the base calls it
// with no dependencies. Two declarations on one table in one package share
// the base, so they pass the same mapper. Only PostgreSQL tables have a
// base.
//
// Postgres.Scopes in nanogen/project.go names the tenant columns. A table is
// scoped when it has one of them and its model field is not a pointer (NOT
// NULL). A hand-written function that touches a scoped table through its
// base (a predicate, a helper, a SELECT, UPDATE or DELETE built on b.t, or
// a SELECT that reads FROM b.t or a join on it) without a predicate or a
// reference on a scope column gets a warning, unless its name ends in
// Internal. Warnings do not fail the run.
//
// Each method of the interface bound by nanorepo.Implement resolves in this
// order: a method of the implementation, in a file nanostack gen repo did not
// write or promoted from an embedded field, wins and must have the
// interface's signature; else a name of the grammar derives; else the run
// fails at the method, with the signature to write by hand and a derivable
// rename when one works. The grammar: FindBy, GetBy, ExistsBy, CountBy and
// DeleteBy followed by Go field names of the table joined by And, FindAll
// and Count; ctx first, then one parameter per column bound by position, of
// the column's model type or a named type over it. A single-row find derives
// only when its columns cover a key of the table's UniqueKeys. On a scoped
// table a name filters on a scope column first; Internal skips the rule on a
// read only. <file>_repo.gen.go holds the derived methods, each one helper
// call of the base with its SQL in the comment, and var _ Iface = (*impl)(nil)
// per binding, so a deleted hand-written method fails the build.
//
// A nanorepo.Search declares a search (ADR 0027). nanostack gen repo reads it and
// checks it: the tables it reads (the table, or each table of its From
// function with its join kind, every column of a LEFT JOIN table nullable),
// their ColumnTypes, ForeignKeys, UniqueKeys and models, its options, and its
// tiebreak (the primary key, or a Tiebreak that covers a unique key that
// orders every row). -explain prints what it read. Then it writes the
// builder of the search into <file>_repo.gen.go of the declaring file, after
// the code of the bindings (design 4.2): the builder type <Name>Search, its
// scope value (the type of the one Scope field, the struct <Name>Scope for
// two or more, struct{} for none), the sort enum <Name>Sort and one constant
// per sort_by value, the spec variable <name>Search built with
// searchexec.DefineSpec (From, the columns that the Row selects, the tenant
// conditions, the conditions, a keyset key per sort and the tiebreak, the
// default sort and limit), New<Name>Search, one accessor per filter, and
// FullText. The Row selects its columns: the AllColumns of the table for its
// model, else the AllColumns of each embedded model and the column of each
// field tagged alias:"table.column". A shape it cannot write yet (SortExpr,
// FilterExists) is a problem of the run, and -explain still prints the
// search.
//
// Generation loads the package with every file nanostack gen repo wrote replaced
// by an empty file, so a stale output never blocks a run and a method a
// previous run derived never counts as hand-written, resolves every method,
// then type-checks the package with the new files: a compile error that
// remains is reported at its line, beside the methods that do not resolve,
// and nothing is written. ForwardStubs gives the pipeline steps before repo
// a stub of each derived file: its methods by parsing alone, and the
// builders of its searches as a run writes them.
package genrepo

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// FileName is the file nanostack gen repo writes into the declaring package.
const FileName = "repo_base.gen.go"

// header starts every file nanostack gen repo writes; the pipeline's orphan scan
// reads it.
const header = "// Code generated by nanostack gen repo. DO NOT EDIT.\n\n"

// Options configures one run.
type Options struct {
	// Dir is the package directory that holds the declarations.
	Dir string
	// Scopes are the tenant columns, Postgres.Scopes of nanogen/project.go.
	Scopes []string
	// ContextScopes maps each context tenant column to the function that
	// scopes it, written as its import path, a dot and its name:
	// Postgres.ContextScopes of nanogen/project.go.
	ContextScopes map[string]string
	// Spec is the OpenAPI document, API.Spec of nanogen/project.go. A
	// search that declares nanorepo.API is checked against it both ways,
	// and gets its API mapper. Empty: such a search is an error (A7).
	Spec string
	// SortByDirection is API.SortByDirection of nanogen/project.go: the
	// direction, ASC or DESC, of a request that sends a sort_by and no
	// sort_direction. Required when a request body has a sort_by.
	SortByDirection string
	// Check compares instead of writing and returns an error wrapping
	// gen.ErrStale on drift.
	Check bool
	// Overlay replaces files in the package loads, keyed by absolute path,
	// as in golang.org/x/tools/go/packages. repo_base.gen.go is always
	// replaced: by an empty file, then by the new base for the final type
	// check.
	Overlay map[string][]byte
}

// Result is the outcome of a generation: the files to write, the warnings
// of the hand-written scope check, which do not fail the run, and how each
// method of each bound interface resolved.
type Result struct {
	// Files are repo_base.gen.go, then one <file>_repo.gen.go per declaring
	// file with a nanorepo.Implement, by name.
	Files    []gen.File
	Warnings []*gen.Error
	// Explain holds one entry per nanorepo.Implement, in source order. It is
	// set also when resolution fails, so -explain shows the failing methods.
	Explain []Explanation
	// Searches holds one entry per nanorepo.Search, in source order, once
	// every declaration reads without a problem.
	Searches []SearchExplanation
	// Exemptions are the tenant exemptions of the searches, in source
	// order, set when the run succeeds. nanostack gen writes them, for every
	// package of the module, to ExemptionsFile.
	Exemptions []Exemption
}

// Generate returns the generated files without touching the disk. Errors
// are *gen.Error values joined with errors.Join, at paths relative to Dir.
// Once the declarations resolve, the Result comes back with its Explain
// even when the error is not nil.
func Generate(opts Options) (*Result, error) {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	l, err := loadPackage(opts)
	if err != nil {
		return nil, err
	}
	l.scopes, l.contextScopes, l.sortByDirection = opts.Scopes, opts.ContextScopes, opts.SortByDirection
	if opts.Spec != "" {
		if l.specPath, err = filepath.Abs(opts.Spec); err != nil {
			return nil, gen.EnvErrorf("pass -spec with a path that exists", "resolve %s: %v", opts.Spec, err)
		}
	}
	decls, err := l.declarations()
	if err != nil {
		return nil, err
	}
	res := l.resolve(decls, opts.Scopes)
	result := &Result{Searches: l.explainSearches(decls)}
	problems := l.unusedSearches(decls, res)
	var missing []string
	failing := false
	for _, r := range res {
		result.Explain = append(result.Explain, r.explain)
		problems = append(problems, r.problems...)
		missing = append(missing, r.missing...)
		failing = failing || r.incomplete || len(r.problems) > 0
	}
	for _, s := range decls.searches {
		if len(s.unbuilt) > 0 {
			problems = append(problems, s.unbuilt...)
			failing = true
			for _, name := range s.builder.names() {
				missing = append(missing, "undefined: "+name)
			}
		}
	}
	out, err := l.renderAll(decls, res)
	if err != nil {
		return result, err
	}
	if targets := l.checkTargets(decls, out); len(targets) > 0 {
		return result, errors.Join(targets...)
	}
	pkg, compile, err := l.verify(decls, out, failing, missing)
	if err != nil {
		return result, err
	}
	if problems = append(problems, compile...); len(problems) > 0 {
		return result, errors.Join(sortProblems(problems)...)
	}
	result.Files = out.files
	result.Exemptions = exemptions(decls)
	result.Warnings = l.scopeWarnings(pkg, decls, opts.Scopes)
	return result, nil
}

// renderAll renders the base and the derived files.
func (l *loaded) renderAll(decls *declarations, res []*resolution) (*rendered, error) {
	base, err := l.render(decls)
	if err != nil {
		return nil, err
	}
	derived, out, err := l.renderDerived(res, decls.searches)
	if err != nil {
		return nil, err
	}
	mappers, err := l.renderMappers(decls.searches, false)
	if err != nil {
		return nil, err
	}
	out.files = append(append([]gen.File{{Path: l.outPath, Data: base}}, derived...), mappers...)
	return out, nil
}

// checkTargets refuses to replace a file that nanostack gen repo did not write,
// such as a hand-written file named like an output.
func (l *loaded) checkTargets(decls *declarations, out *rendered) []error {
	var problems []error
	for _, f := range out.files {
		if l.isOwn(f.Path) || (filepath.Dir(f.Path) != l.dir && l.hasHeader(f.Path)) {
			continue
		}
		if _, inOverlay := l.overlay[f.Path]; !inOverlay {
			if _, err := os.Stat(f.Path); err != nil {
				continue
			}
		}
		o := at{decls.tables[0].file, decls.tables[0].line}
		what := "the declarations of this package"
		if res := out.byFile[filepath.Base(f.Path)]; len(res) > 0 {
			o, what = at{res[0].b.file, res[0].b.line}, "the declarations in "+res[0].b.file
		} else if searches := out.searches[filepath.Base(f.Path)]; len(searches) > 0 {
			o, what = searches[0].at, "the declarations in "+searches[0].at.file
		} else if s := mapperOf(decls.searches, f.Path); s != nil {
			o, what = s.at, "search "+s.name
		}
		problems = append(problems, gen.Errorf(
			o.file,
			o.line,
			"rename that file: nanostack gen repo writes "+filepath.Base(f.Path)+" for "+what,
			"%s exists, and nanostack gen repo did not write it (it has no nanostack gen repo header)",
			filepath.Base(f.Path),
		))
	}
	return problems
}

// sortProblems orders problems by file and line, keeping the order of
// problems on one line.
func sortProblems(problems []error) []error {
	key := func(err error) (string, int) {
		if e, ok := errors.AsType[*gen.Error](err); ok {
			return e.File, e.Line
		}
		return "", 0
	}
	slices.SortStableFunc(problems, func(a, b error) int {
		fa, la := key(a)
		fb, lb := key(b)
		return cmp.Or(strings.Compare(fa, fb), cmp.Compare(la, lb))
	})
	return problems
}

// Run generates the files, writes those that changed and deletes the files
// with the nanostack gen repo header that the run no longer produces. With Check
// it writes and deletes nothing and returns an error wrapping gen.ErrStale
// when a file on disk differs, or a problem per orphan. The warnings come
// back on success.
func Run(opts Options) ([]*gen.Error, error) {
	_, _, warnings, err := run(opts)
	return warnings, err
}

func run(opts Options) (written, deleted []string, warnings []*gen.Error, err error) {
	res, err := Generate(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	written, err = gen.Sync("repo", res.Files, opts.Check)
	if err != nil && !errors.Is(err, gen.ErrStale) {
		return nil, nil, nil, err
	}
	deleted, orphanErr := orphans(opts, res.Files)
	if err = errors.Join(err, orphanErr); err != nil {
		return nil, nil, nil, err
	}
	return written, deleted, res.Warnings, nil
}

// orphans deletes, or with Check reports, the files of the package
// directory with the nanostack gen repo header that files does not hold: the
// derived file of a declaring file that is gone or binds nothing any more.
func orphans(opts Options, files []gen.File) ([]string, error) {
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, gen.EnvErrorf("run from a directory that exists", "resolve %s: %v", opts.Dir, err)
	}
	l := &loaded{dir: dir}
	l.findOwn()
	produced := map[string]bool{}
	for _, f := range files {
		produced[f.Path] = true
	}
	var deleted []string
	var problems []error
	for _, path := range l.own {
		if produced[path] {
			continue
		}
		rel := filepath.Base(path)
		if opts.Check {
			problems = append(
				problems,
				gen.Errorf(rel, 0, "run nanostack gen repo, which deletes it, and commit the result",
					"orphan: no declaration of the package produces it any more"),
			)
			continue
		}
		if err = os.Remove(path); err != nil {
			return deleted, &gen.Error{File: path, Msg: err.Error(), Fix: "make the file deletable",
				Err: gen.ErrEnvironment}
		}
		deleted = append(deleted, path)
	}
	return deleted, errors.Join(problems...)
}

// Main runs `nanostack gen repo [-dir d] [-scopes a,b] [-context-scopes c=pkg.F] [-check] [-explain
// [Iface|Search...]]` and returns the process exit code: 0 on success, also with
// warnings, 1 on stale output or a declaration error, 2 on bad flags, 3 when
// the environment fails; -h prints the flags and returns 0. Warnings print to
// stderr, each as "warning: file:line: msg" and a fix line. -explain prints
// how each method of each bound interface, or of the interfaces named,
// resolves, to stdout. It is a gen.Command.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nanostack gen repo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts Options
	var scopes, contextScopes string
	var explain bool
	fs.StringVar(&opts.Dir, "dir", ".", "package directory holding the nanorepo declarations")
	fs.StringVar(&scopes, "scopes", "",
		"comma-separated tenant columns, as Postgres.Scopes in nanogen/project.go (nanostack gen passes them)")
	fs.StringVar(&contextScopes, "context-scopes", "",
		"comma-separated column=importpath.Func pairs, as Postgres.ContextScopes in nanogen/project.go")
	fs.StringVar(&opts.Spec, "spec", "",
		"the OpenAPI document, as API.Spec in nanogen/project.go (nanostack gen passes it)")
	fs.StringVar(&opts.SortByDirection, "sort-by-direction", "",
		"ASC or DESC: the direction of a sort_by sent alone, as API.SortByDirection (nanostack gen passes it)")
	fs.BoolVar(&opts.Check, "check", false, "write nothing; exit 1 when an output is stale or orphaned")
	fs.BoolVar(&explain, "explain", false,
		"print how each method of each bound interface resolves and what each search reads; "+
			"name interfaces or searches after the flags to filter")
	if exit, ok := gen.ParseFlags(fs, args); !ok {
		return exit
	}
	if fs.NArg() > 0 && !explain {
		_, _ = fmt.Fprintf(stderr, "nanostack gen repo: unexpected arguments %v; only -explain takes interface or "+
			"search names\n",
			fs.Args())
		return 2
	}
	for s := range strings.SplitSeq(scopes, ",") {
		if s = strings.TrimSpace(s); s != "" {
			opts.Scopes = append(opts.Scopes, s)
		}
	}
	for pair := range strings.SplitSeq(contextScopes, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		col, fn, ok := strings.Cut(pair, "=")
		if !ok || col == "" || fn == "" {
			_, _ = fmt.Fprintf(
				stderr,
				"nanostack gen repo: -context-scopes entry %q is not column=importpath.Func\n",
				pair,
			)
			return 2
		}
		if opts.ContextScopes == nil {
			opts.ContextScopes = map[string]string{}
		}
		opts.ContextScopes[col] = fn
	}
	if explain {
		return mainExplain(opts, fs.Args(), stdout, stderr)
	}
	written, deleted, warnings, err := run(opts)
	for _, w := range warnings {
		_, _ = fmt.Fprintln(stderr, "warning: "+w.Error())
	}
	if err != nil {
		return gen.Report(stderr, "repo", err)
	}
	for _, path := range written {
		_, _ = fmt.Fprintf(stdout, "nanostack gen repo: wrote %s\n", path)
	}
	for _, path := range deleted {
		_, _ = fmt.Fprintf(stdout, "nanostack gen repo: deleted %s\n", path)
	}
	return 0
}

// mainExplain prints the resolution of each bound interface and what each
// search reads, or only those named, and writes nothing.
func mainExplain(opts Options, names []string, stdout, stderr io.Writer) int {
	res, err := Generate(opts)
	if res != nil {
		var shown []Explanation
		for _, e := range res.Explain {
			if len(names) == 0 || slices.Contains(names, e.Iface) {
				shown = append(shown, e)
			}
		}
		var searches []SearchExplanation
		for _, e := range res.Searches {
			if len(names) == 0 || slices.Contains(names, e.Name) {
				searches = append(searches, e)
			}
		}
		if len(shown) == 0 && len(searches) == 0 && err == nil {
			_, _ = fmt.Fprintf(stderr, "nanostack gen repo: no nanorepo.Implement binds %s, and no nanorepo.Search "+
				"has that name\n", strings.Join(names, ", "))
			return 1
		}
		WriteExplain(stdout, shown)
		WriteSearchExplain(stdout, searches)
	}
	if err != nil {
		return gen.Report(stderr, "repo", err)
	}
	return 0
}

// WriteExplain writes the explanations as nanostack gen repo -explain prints
// them: per interface a header line, then one line per method with how it
// resolves, and a note line when there is one.
func WriteExplain(w io.Writer, explain []Explanation) {
	for _, e := range explain {
		scope := ""
		if e.Scope != "" {
			scope = ", scope " + e.Scope
		}
		_, _ = fmt.Fprintf(w, "%s on *%s (table %s%s)\n", e.Iface, e.Impl, e.Table, scope)
		width := 0
		for _, m := range e.Methods {
			width = max(width, len(m.Name))
		}
		for _, m := range e.Methods {
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, m.Name, m.How)
			if m.Note != "" {
				_, _ = fmt.Fprintf(w, "  %-*s  note: %s\n", width, "", m.Note)
			}
		}
	}
}

// Declares reports whether f, parsed alone, declares a repository: it
// imports db/nanorepo and calls its Implement, Base or Search. The pipeline
// discovers the packages of nanostack gen repo with it, on a tree that may not
// compile.
func Declares(f *ast.File) bool {
	name := nanorepoName(f)
	if name == "" {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		fun := call.Fun
		switch x := fun.(type) {
		case *ast.IndexExpr:
			fun = x.X
		case *ast.IndexListExpr:
			fun = x.X
		}
		if sel, isSel := fun.(*ast.SelectorExpr); isSel && (sel.Sel.Name == "Implement" ||
			sel.Sel.Name == "Base" || sel.Sel.Name == "Search") {
			x, isIdent := sel.X.(*ast.Ident)
			found = isIdent && x.Name == name
		}
		return !found
	})
	return found
}
