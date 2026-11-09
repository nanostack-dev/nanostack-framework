package genrepo

import (
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

// The scope column moves to the front of a derived name.
func TestSuggestScoped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		preds []string
		want  string
	}{
		{"FindByEmail", nil, "FindByProductIDAndEmail"},
		{"FindByIDAndProductID", []string{"ID", "ProductID"}, "FindByProductIDAndID"},
		{"DeleteByEmail", []string{"Email"}, "DeleteByProductIDAndEmail"},
		{"FindAll", nil, "FindByProductID"},
		{"Count", nil, "CountByProductID"},
	} {
		if got := suggestScoped(tc.name, "ProductID", tc.preds...); got != tc.want {
			t.Errorf("suggestScoped(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
