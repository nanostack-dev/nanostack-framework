package genrepo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

)

// writeKind is the kind of a derived write by its name, "" for a read.
func writeKind(name string) string {
	for _, k := range []string{"Create", "Update", "Upsert"} {
		if strings.HasPrefix(name, k) {
			return k
		}
	}
	return ""
}

// writeViolations reads the derived file src and returns, for each derived
// write, the scope columns of its table that the code that runs leaves to
// the value. A write never relies on Postgres.ScopeImplies, so every scope
// column must be covered by itself:
//   - a Create stamps every scope column into the row (row.X = ...);
//   - an Update has every scope column in its WHERE;
//   - an Upsert stamps every scope column, and every scope column is in its
//     conflict target or in the WHERE guard of its DO UPDATE.
func writeViolations(t *testing.T, src []byte, scopes map[string][]string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "derived.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Body == nil || writeKind(fd.Name.Name) == "" {
			continue
		}
		star, isStar := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !isStar {
			continue
		}
		recv, _ := star.X.(*ast.Ident)
		required, scoped := scopes[recv.Name]
		if !scoped {
			continue
		}
		stamped, guarded := map[string]bool{}, map[string]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt: // row.OrgID = orgID, row.WorkspaceID, err = Stamp(ctx)
				for _, lhs := range n.Lhs {
					if sel, isSel := lhs.(*ast.SelectorExpr); isSel {
						if id, isID := sel.X.(*ast.Ident); isID && id.Name == "row" {
							stamped[sel.Sel.Name] = true
						}
					}
				}
			case *ast.CallExpr:
				if sel, isSel := n.Fun.(*ast.SelectorExpr); isSel && (sel.Sel.Name == "WHERE" || sel.Sel.Name == "ON_CONFLICT") {
					for _, arg := range n.Args {
						ast.Inspect(arg, func(m ast.Node) bool {
							if s, isS := m.(*ast.SelectorExpr); isS {
								if in, isIn := s.X.(*ast.SelectorExpr); isIn && (in.Sel.Name == "where" || in.Sel.Name == "t") {
									guarded[s.Sel.Name] = true
								}
							}
							return true
						})
					}
				}
			}
			return true
		})
		for _, col := range required {
			field := goField(col)
			switch writeKind(fd.Name.Name) {
			case "Create":
				if !stamped[field] {
					out = append(out, fd.Name.Name+": does not stamp "+col)
				}
			case "Update":
				if !guarded[field] {
					out = append(out, fd.Name.Name+": its WHERE leaves out "+col)
				}
			case "Upsert":
				if !stamped[field] {
					out = append(out, fd.Name.Name+": does not stamp "+col)
				}
				if !guarded[field] {
					out = append(out, fd.Name.Name+": its conflict target and DO UPDATE guard leave out "+col)
				}
			}
		}
	}
	return out
}

// mutateIn replaces from by to inside the method of the receiver impl called
// name, and reports whether it found from there.
func mutateIn(src, impl, name, from, to string) (string, bool) {
	head := "func (r *" + impl + ") " + name + "("
	start := strings.Index(src, head)
	if start < 0 {
		return src, false
	}
	end := len(src)
	if next := strings.Index(src[start+1:], "\nfunc "); next >= 0 {
		end = start + 1 + next
	}
	body := src[start:end]
	if !strings.Contains(body, from) {
		return src, false
	}
	return src[:start] + strings.Replace(body, from, to, 1) + src[end:], true
}

// The write methods of the torture fixture leave no scope column to the
// value, and a dropped scope in any write shape is caught: the WHERE of an
// update, the stamped columns of a create, the conflict target and the DO
// UPDATE guard of an upsert.
func TestTortureEveryDerivedWriteCoversEveryScope(t *testing.T) {
	t.Parallel()
	res, err := Generate(tortureOptions(tortureFixture))
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string][]string{}
	for _, ex := range res.Explain {
		for col := range strings.SplitSeq(ex.Scope, ", ") {
			if col, _, _ = strings.Cut(col, " "); col != "" {
				scopes[ex.Impl] = append(scopes[ex.Impl], col)
			}
		}
	}
	var file string
	for _, f := range res.Files {
		if strings.HasSuffix(f.Path, "writes"+derivedSuffix) {
			file = string(f.Data)
		}
	}
	if file == "" {
		t.Fatal("no derived file of the writes in the torture fixture")
	}
	if bad := writeViolations(t, []byte(file), scopes); len(bad) > 0 {
		t.Fatalf("a derived write leaves a scope column to the value:\n%s", strings.Join(bad, "\n"))
	}
	if n := strings.Count(file, "is derived from its name"); n < 8 {
		t.Errorf("%d derived writes and finds checked, want at least 8", n)
	}
	const (
		wides = "widesImpl"
		shelf = "wShelvesImpl"
		upW   = "UpsertByOrgAndProductAndWorkspaceAndSlug"
		upD   = "UpdateByOrgAndProductAndWorkspaceAndID"
		crt   = "CreateInOrgAndProductAndWorkspace"
	)
	mutations := []struct{ impl, method, from, to, what string }{
		{wides, upD, ".AND(r.where.ProductID(productID))", "", "update WHERE drops product_id"},
		{wides, upD, "r.where.OrgID(orgID).AND(", "(", "update WHERE drops org_id"},
		{wides, upD, ".AND(workspacescope.Filter(ctx, r.t.WorkspaceID))", "", "update WHERE drops the context scope"},
		{shelf, upD, ".AND(r.where.ProductID(productID))", "", "update without a returning value drops product_id"},
		{wides, crt, "row.ProductID = productID", "_ = productID", "create does not stamp product_id"},
		{wides, crt, "row.OrgID = orgID", "_ = orgID", "create does not stamp org_id"},
		{wides, crt, "row.WorkspaceID, err = workspacescope.Stamp(ctx)", "_, err = workspacescope.Stamp(ctx)", "create does not stamp the workspace"},
		{wides, upW, "row.OrgID = orgID", "_ = orgID", "upsert does not stamp org_id"},
		{wides, upW, "ON_CONFLICT(r.t.OrgID, ", "ON_CONFLICT(", "upsert conflict target drops org_id"},
		{wides, upW, ").WHERE(r.t.WorkspaceID.EQ(r.t.EXCLUDED.WorkspaceID)))", "))", "upsert drops the workspace guard"},
		{shelf, upW, "r.t.OrgID.EQ(r.t.EXCLUDED.OrgID).AND(", "(", "upsert guard drops org_id"},
	}
	for _, m := range mutations {
		mutated, found := mutateIn(file, m.impl, m.method, m.from, m.to)
		if !found {
			t.Errorf("mutation %q: the emitted code has no %q in %s", m.what, m.from, m.method)
			continue
		}
		if bad := writeViolations(t, []byte(mutated), scopes); len(bad) == 0 {
			t.Errorf("mutation %q is not caught", m.what)
		}
	}
	if len(mutations) < 10 || !slices.Contains(scopes[wides], "org_id") {
		t.Errorf("%d mutations, scopes %v", len(mutations), scopes)
	}
}
