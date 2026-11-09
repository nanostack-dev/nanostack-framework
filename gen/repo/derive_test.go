package genrepo

import (
	"go/token"
	"go/types"
	"slices"
	"testing"
)

// A key proves a condition when every one of its columns is in it; a key
// on a column the table lost never proves.
func TestProvingKey(t *testing.T) {
	t.Parallel()
	tb := &tableBase{keys: []uniqueKey{
		{name: "pkey", fields: []string{"ID"}},
		{name: "email", fields: []string{"ProductID", "Email"}},
		{name: "wide", fields: []string{"ProductID", "Status", "Name", "CreatedAt"}},
		{name: "gone", fields: []string{"ProductID", ""}},
	}}
	for _, tc := range []struct {
		where []string
		want  string
	}{
		{[]string{"ProductID", "ID"}, "pkey"},
		{[]string{"ProductID", "Email"}, "email"},
		{[]string{"Email", "ProductID", "Name"}, "email"},
		{[]string{"ProductID", "Status", "Name"}, ""},
		{[]string{"ProductID", "Status", "Name", "CreatedAt"}, "wide"},
		{[]string{"ProductID"}, ""},
	} {
		k, ok := tb.provingKey(tc.where)
		if got := map[bool]string{true: k.name}[ok]; got != tc.want {
			t.Errorf("provingKey(%v) = %q, want %q", tc.where, got, tc.want)
		}
	}
}

// Properties split on whole column names, longest first, so a column whose
// name holds And still parses.
func TestSplitPreds(t *testing.T) {
	t.Parallel()
	fields := []string{"ID", "ProductID", "BrandAndModel", "Brand", "Email"}
	for in, want := range map[string][]string{
		"ProductIDAndEmail":     {"ProductID", "Email"},
		"BrandAndModelAndEmail": {"BrandAndModel", "Email"},
		"BrandAndEmail":         {"Brand", "Email"},
		"ProductIDAndMail":      nil,
		"ProductIDEmail":        nil,
	} {
		got, ok := splitPreds(in, fields)
		if !slices.Equal(got, want) || ok != (want != nil) {
			t.Errorf("splitPreds(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
}

// A parameter name spells a column in its usual Go forms; a name that is
// no column binds by position.
func TestColumnNamed(t *testing.T) {
	t.Parallel()
	fields := []string{"ID", "ProductID", "ExternalID", "PlatformTenantID"}
	for param, want := range map[string]string{
		"id": "ID", "productID": "ProductID", "productId": "ProductID", "externalID": "ExternalID",
		"tenantID": "", "email": "", "": "",
	} {
		if got := columnNamed(param, fields); got != want {
			t.Errorf("columnNamed(%q) = %q, want %q", param, got, want)
		}
	}
}

// The suggested column for a name that is no column.
func TestSuggest(t *testing.T) {
	t.Parallel()
	cols := []string{"ID", "ProductID", "Email", "ExternalRef", "CreatedAt", "CreatedBy", "PlatformTenantID",
		"OrganizationID"}
	for seg, want := range map[string]string{
		"ProductId":   "ProductID",
		"TenantID":    "PlatformTenantID",
		"OrgID":       "OrganizationID",
		"Emial":       "Email",
		"ExternalID":  "ExternalRef",
		"CreatedTime": "",
	} {
		if got := suggest(seg, cols); got != want {
			t.Errorf("suggest(%q) = %q, want %q", seg, got, want)
		}
	}
}

// A suggested name puts the scope columns first, by their short names when
// the project gives them.
func TestSuggestNamed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		words []string
		want  string
	}{
		{"FindByEmail", []string{"ProductID", "Email"}, "FindByProductIDAndEmail"},
		{"DeleteByEmail", []string{"ProductID", "Email"}, "DeleteByProductIDAndEmail"},
		{"FindAll", []string{"ProductID"}, "FindByProductID"},
		{"Count", []string{"Org", "Workspace"}, "CountByOrgAndWorkspace"},
		{"FindByID", []string{"Org", "Workspace", "ID"}, "FindByOrgAndWorkspaceAndID"},
	} {
		if got := suggestNamed(tc.name, tc.words); got != tc.want {
			t.Errorf("suggestNamed(%q, %v) = %q, want %q", tc.name, tc.words, got, tc.want)
		}
	}
}

// A short name is a word of a derived name beside the Go fields, and the
// longest word wins: the field ProductID beats the short name Product.
func TestSplitTerms(t *testing.T) {
	t.Parallel()
	toks := []word{{"ID", "ID"}, {"OrganizationID", "OrganizationID"}, {"ProductID", "ProductID"},
		{"WorkspaceID", "WorkspaceID"}, {"Org", "OrganizationID"}, {"Product", "ProductID"},
		{"Workspace", "WorkspaceID"}}
	for in, want := range map[string][]string{
		"OrgAndWorkspaceAndID":              {"OrganizationID", "WorkspaceID", "ID"},
		"OrganizationIDAndWorkspaceIDAndID": {"OrganizationID", "WorkspaceID", "ID"},
		"ProductIDAndID":                    {"ProductID", "ID"},
		"ProductAndID":                      {"ProductID", "ID"},
		"OrgAndProductIDAndWorkspace":       {"OrganizationID", "ProductID", "WorkspaceID"},
		"OrgID":                             nil,
		"OrgAndNope":                        nil,
	} {
		got, ok := splitTerms(in, toks)
		fields := make([]string, len(got))
		for i, g := range got {
			fields[i] = g.field
		}
		if ok != (want != nil) || (ok && !slices.Equal(fields, want)) {
			t.Errorf("splitTerms(%q) = %v, %v; want %v", in, fields, ok, want)
		}
	}
	// The text of the token that matched tells a short name from a field.
	got, _ := splitTerms("ProductIDAndProduct", toks)
	if len(got) != 2 || got[0].text != "ProductID" || got[1].text != "Product" {
		t.Errorf("longest first: %v", got)
	}
}

// parseName reads short names, binds no parameter for a context scope named
// by its short name, and applies the scope rules.
func TestParseNameScopeNames(t *testing.T) {
	t.Parallel()
	tb := &tableBase{sqlName: "hooks", columns: []column{
		{field: "ID", sqlName: "id"},
		{field: "OrganizationID", sqlName: "organization_id"},
		{field: "WorkspaceID", sqlName: "workspace_id", nullable: true},
		{field: "Name", sqlName: "name"},
	}}
	l := &loaded{
		scopes:        []string{"organization_id", "workspace_id"},
		contextScopes: map[string]string{"workspace_id": "example.com/app/workspacescope.Filter"},
		scopeNames:    map[string]string{"organization_id": "Org", "workspace_id": "Workspace"},
	}
	r := &resolver{l: l, t: tb, scope: tb.scopeFields(l.scopes)}
	r.setupScopes(l.scopes)
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, tc := range []struct {
		name     string
		preds    []string
		ctx      []bool // per term: bound to the context function
		unscoped bool
		skipped  []string
	}{
		{"FindByOrgAndWorkspaceAndID", []string{"OrganizationID", "ID"}, []bool{false, true, false}, false, nil},
		{"FindByOrganizationIDAndWorkspaceIDAndID", []string{"OrganizationID", "WorkspaceID", "ID"},
			[]bool{false, false, false}, false, nil},
		{"FindByOrgAndWorkspaceIDAndName", []string{"OrganizationID", "WorkspaceID", "Name"},
			[]bool{false, false, false}, false, nil},
		{"FindByOrgAndIDInternal", []string{"OrganizationID", "ID"}, []bool{false, false}, true,
			[]string{"WorkspaceID"}},
		{"FindByIDInternal", []string{"ID"}, []bool{false}, true, []string{"OrganizationID", "WorkspaceID"}},
		{"FindAllInternal", nil, nil, true, []string{"OrganizationID", "WorkspaceID"}},
		{"CountByOrgAndWorkspace", []string{"OrganizationID"}, []bool{false, true}, false, nil},
	} {
		m, fail := r.parseName(types.NewFunc(token.NoPos, nil, tc.name, sig))
		if fail != nil {
			t.Errorf("%s: %s", tc.name, fail.msg)
			continue
		}
		var ctx []bool
		for _, tm := range m.terms {
			ctx = append(ctx, tm.ctx != nil)
		}
		if !slices.Equal(m.preds, tc.preds) || !slices.Equal(ctx, tc.ctx) || m.unscoped != tc.unscoped ||
			!slices.Equal(m.skipped, tc.skipped) {
			t.Errorf("%s: preds %v, ctx %v, unscoped %v %v; want %v, %v, %v %v", tc.name, m.preds, ctx,
				m.unscoped, m.skipped, tc.preds, tc.ctx, tc.unscoped, tc.skipped)
		}
	}
}

// A trailing Internal is the scope rule's suffix, unless the name splits
// into columns with it: a column such as IsInternal keeps it.
func TestInternalSuffix(t *testing.T) {
	t.Parallel()
	r := &resolver{t: &tableBase{columns: []column{{field: "Name"}, {field: "IsInternal"}}}}
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, tc := range []struct {
		name     string
		preds    []string
		internal bool
	}{
		{"FindByIsInternal", []string{"IsInternal"}, false},
		{"FindByNameAndIsInternal", []string{"Name", "IsInternal"}, false},
		{"FindByNameInternal", []string{"Name"}, true},
		{"FindByIsInternalInternal", []string{"IsInternal"}, true},
		{"FindAllInternal", nil, true},
	} {
		m, fail := r.parseName(types.NewFunc(token.NoPos, nil, tc.name, sig))
		if fail != nil {
			t.Errorf("%s: %s", tc.name, fail.msg)
			continue
		}
		if !slices.Equal(m.preds, tc.preds) || m.internal != tc.internal {
			t.Errorf("%s: properties %v, internal %v; want %v, %v", tc.name, m.preds, m.internal, tc.preds, tc.internal)
		}
	}
}
