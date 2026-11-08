package oapi

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/oapitemplates"
)

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

// serverConfig is the part of oapi-codegen's configuration the server
// config sets, with oapi-codegen's YAML keys.
type serverConfig struct {
	Package  string          `yaml:"package"`
	Generate map[string]bool `yaml:"generate"`
	Imports  []struct {
		Package string `yaml:"package"`
	} `yaml:"additional-imports"`
	OutputOptions struct {
		UserTemplates map[string]string `yaml:"user-templates"`
	} `yaml:"output-options"`
}

// The rendered config generates the ServeMux strict server through the
// framework's template override: NewHTTPHandler exists only with
// std-http-server, and the override needs nanohttp and problem imported.
func TestServerConfigUsesTheBoundary(t *testing.T) {
	out, err := yaml.Marshal(DefaultServiceConfig("apigen").ServerCodegenYAML())
	if err != nil {
		t.Fatal(err)
	}
	var got serverConfig
	if err = yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Generate["std-http-server"] || !got.Generate["strict-server"] || !got.Generate["models"] {
		t.Errorf("generate = %v", got.Generate)
	}
	for _, router := range []string{"chi-server", "gorilla-server", "echo-server", "gin-server", "fiber-server"} {
		if _, ok := got.Generate[router]; ok {
			t.Errorf("generate sets %s; the boundary is net/http's ServeMux", router)
		}
	}
	var imports []string
	for _, imp := range got.Imports {
		imports = append(imports, imp.Package)
	}
	if len(imports) != 2 || imports[0] != nanohttpPackage || imports[1] != problemPackage {
		t.Errorf("additional-imports = %v", imports)
	}
	want := oapitemplates.UserTemplates()["strict/strict-http.tmpl"]
	if tmpl := got.OutputOptions.UserTemplates["strict/strict-http.tmpl"]; tmpl == "" || tmpl != want {
		t.Errorf("user-templates does not carry the strict override inline (%d bytes)", len(tmpl))
	}
}
