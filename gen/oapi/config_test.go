package oapi

import "testing"

func TestDefaultServiceConfig(t *testing.T) {
	cfg := DefaultServiceConfig("apigen")
	if cfg.SpecPath != "cmd/http/openapi.yaml" {
		t.Fatalf("unexpected spec path %q", cfg.SpecPath)
	}
	server := cfg.ServerCodegenYAML()
	if server["package"] != "apigen" {
		t.Fatalf("unexpected server package %+v", server)
	}
}
