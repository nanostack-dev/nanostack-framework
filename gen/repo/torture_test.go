package genrepo

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/gentest"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/torture"
)

const tortureFixture = "internal/torture"

// tortureOptions are the options of a package that declares tables of the
// torture fixture: three hard tenant columns, one context column, and their
// short names.
func tortureOptions(dir string) Options {
	return Options{
		Dir: dir, Scopes: torture.Scopes, ContextScopes: torture.ContextScopes, ScopeNames: torture.ScopeNames,
		ScopeImplies: torture.ScopeImplies, ContextStamps: torture.ContextStamps, DatabaseColumns: torture.DatabaseColumns,
	}
}

// The committed base and derived files of the torture fixture, whose
// integration tests run on PostgreSQL, are what the generator writes, and it
// gets no warning: every method of the fixture derives.
func TestTortureBaseIsCurrent(t *testing.T) {
	t.Parallel()
	res, err := Generate(tortureOptions(tortureFixture))
	if err != nil {
		gentest.Problems(t, err)
		t.Fatal(err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("warnings on the torture fixture: %v", res.Warnings)
	}
	for _, f := range res.Files {
		golden(t, f.Path, f.Data)
	}
	// Every method of every interface of the fixture derives: none is hand-written.
	for _, ex := range res.Explain {
		for _, m := range ex.Methods {
			if len(m.How) < 7 || m.How[:7] != "derived" {
				t.Errorf("%s.%s is not derived: %s", ex.Iface, m.Name, m.How)
			}
		}
	}
}

// The names that must not derive on the torture tables are refused, each at
// its line, with the fix in testdata/torture/refused/want.txt. The package's
// base is the torture base under another package clause.
func TestTortureRefusals(t *testing.T) {
	t.Parallel()
	dir := "testdata/torture/refused"
	base, err := os.ReadFile(filepath.Join(tortureFixture, FileName))
	if err != nil {
		t.Fatal(err)
	}
	golden(
		t,
		filepath.Join(dir, FileName),
		bytes.Replace(base, []byte("package torture\n"), []byte("package refused\n"), 1),
	)
	res, err := Generate(tortureOptions(dir))
	if err == nil {
		t.Fatal("no refusal")
	}
	gentest.Problems(t, err)
	if gentest.Environment(err) {
		t.Fatalf("an environment error: %v", err)
	}
	golden(t, filepath.Join(dir, "want.txt"), report(nil, err))
	explainGolden(t, filepath.Join(dir, "want-explain.txt"), res)
}

// scopeViolations reads the derived file src and returns, for each derived
// method, the scope columns of its table that its condition covers neither
// by a predicate, nor by the context function, nor by what a covered column
// implies. A method marked UNSCOPED may leave out exactly the columns its
// comment names. It is the mechanical proof that no derived method omits a
// scope column: it reads the code that runs, not the declarations.
func scopeViolations(t *testing.T, src []byte, scopes map[string][]string, implies map[string]string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "derived.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil {
			continue
		}
		star, isStar := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !isStar {
			continue
		}
		recv, _ := star.X.(*ast.Ident)
		required, scoped := scopes[recv.Name]
		if !scoped || writeKind(fd.Name.Name) != "" { // writes: writeViolations
			continue
		}
		covered := map[string]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if sel, isSel := n.(*ast.SelectorExpr); isSel {
				// r.where.OrgID and r.t.WorkspaceID: the field after where or t.
				inner, isInner := sel.X.(*ast.SelectorExpr)
				if isInner && (inner.Sel.Name == "where" || inner.Sel.Name == "t") {
					covered[sel.Sel.Name] = true
				}
			}
			return true
		})
		// A column implied by a covered one is covered too.
		for changed := true; changed; {
			changed = false
			for col, to := range implies {
				if covered[goField(col)] && !covered[goField(to)] {
					covered[goField(to)], changed = true, true
				}
			}
		}
		var skipped string
		if fd.Doc != nil {
			for _, c := range fd.Doc.List {
				if rest, isSkip := strings.CutPrefix(
					c.Text,
					"// UNSCOPED: the name ends in Internal, so it does not filter on ",
				); isSkip {
					skipped = strings.TrimSuffix(rest, ".")
				}
			}
		}
		var missing []string
		for _, col := range required {
			if !covered[goField(col)] {
				missing = append(missing, col)
			}
		}
		if skipped != "" && !strings.HasSuffix(fd.Name.Name, "Internal") {
			out = append(out, fd.Name.Name+": UNSCOPED without Internal")
		}
		if got := strings.Join(missing, " or "); got != skipped {
			out = append(out, fd.Name.Name+": leaves out ["+got+"], its comment says ["+skipped+"]")
		}
	}
	return out
}

// goField is the Go field of a column of the torture tables.
func goField(col string) string {
	var b strings.Builder
	for w := range strings.SplitSeq(col, "_") {
		if w == "id" {
			b.WriteString("ID")
		} else {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}

// No derived method of the torture fixture can omit a scope column: every
// condition that the generator emits covers every scope column of its table,
// or its Internal name says which it skips, and the skipped columns are
// exactly the ones it does not cover. A mutation of the emitted code, which
// removes one predicate, is caught.
func TestTortureEveryDerivedMethodCoversEveryScope(t *testing.T) {
	t.Parallel()
	res, err := Generate(tortureOptions(tortureFixture))
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string][]string{}
	for _, ex := range res.Explain {
		for col := range strings.SplitSeq(ex.Scope, ", ") {
			col, _, _ = strings.Cut(col, " ")
			if col != "" {
				scopes[ex.Impl] = append(scopes[ex.Impl], col)
			}
		}
	}
	if len(scopes) < 10 {
		t.Fatalf("scopes of %d repositories read from -explain, want at least 10: %v", len(scopes), scopes)
	}
	methods, mutations := 0, 0
	for _, f := range res.Files {
		if !strings.HasSuffix(f.Path, derivedSuffix) {
			continue
		}
		methods += strings.Count(string(f.Data), "is derived from its name")
		if bad := scopeViolations(t, f.Data, scopes, torture.ScopeImplies); len(bad) > 0 {
			t.Errorf("%s:\n%s", filepath.Base(f.Path), strings.Join(bad, "\n"))
		}
		// The mutations: each removes a predicate from the code that runs.
		for _, m := range []struct{ from, to string }{
			{".AND(r.where.ProductID(productID)).AND(workspacescope.Filter(ctx, r.t.WorkspaceID)).AND(r.where.ID(id))",
				".AND(workspacescope.Filter(ctx, r.t.WorkspaceID)).AND(r.where.ID(id))"},
			{"workspacescope.Filter(ctx, r.t.WorkspaceID).AND(r.where.Title(title))", "r.where.Title(title)"},
			{"r.where.OrgID(orgID).AND(r.where.Msg(msg))", "r.where.Msg(msg)"},
		} {
			if !strings.Contains(string(f.Data), m.from) {
				continue
			}
			mutations++
			mutated := strings.Replace(string(f.Data), m.from, m.to, 1)
			if bad := scopeViolations(t, []byte(mutated), scopes, torture.ScopeImplies); len(bad) == 0 {
				t.Errorf("removing %q from the emitted code is not caught", m.from)
			}
		}
	}
	if mutations < 3 {
		t.Errorf("%d mutations applied, want 3", mutations)
	}
	if methods < 250 {
		t.Errorf("%d derived methods checked, want at least 250", methods)
	}
}
