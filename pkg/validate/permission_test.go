package validate

import "testing"

func TestIsPermissionName(t *testing.T) {
	valid := []string{"document:create", "product-user:manage", "workspace_1:delete"}
	for _, value := range valid {
		if !IsPermissionName(value) {
			t.Fatalf("expected valid permission %q", value)
		}
	}
	invalid := []string{"", "document", "document:", "Document:create", "document:create:extra"}
	for _, value := range invalid {
		if IsPermissionName(value) {
			t.Fatalf("expected invalid permission %q", value)
		}
	}
}
