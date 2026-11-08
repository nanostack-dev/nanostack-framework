package oapi

import "github.com/nanostack-dev/nanostack-framework/http/nanohttp/oapitemplates"

// Config describes oapi-codegen inputs and outputs without hiding oapi-codegen itself.
type Config struct {
	SpecPath      string
	ServerPackage string
	ServerOutput  string
	ClientPackage string
	ClientOutput  string
	TypesOutput   string
	CleanSpecPath string
}

// Packages the generated server imports through the template override.
const (
	nanohttpPackage = "github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	problemPackage  = "github.com/nanostack-dev/nanostack-framework/http/problem"
)

// ServerCodegenYAML returns the oapi-codegen server config of the HTTP
// boundary: models and a strict server on net/http's ServeMux
// (std-http-server), with the strict template replaced by the override in
// package oapitemplates. The override decodes bodies strictly through
// nanohttp, runs the validators nano gen validate writes, and sends every
// error through problem as RFC 9457 application/problem+json.
//
// The override is pinned to oapitemplates.OapiCodegenVersion; run that
// oapi-codegen release, then nano gen validate on the same package.
func (c Config) ServerCodegenYAML() map[string]any {
	return map[string]any{
		"package": c.ServerPackage,
		"output":  c.ServerOutput,
		"generate": map[string]bool{
			"models":          true,
			"std-http-server": true,
			"strict-server":   true,
		},
		"additional-imports": []map[string]string{
			{"package": nanohttpPackage},
			{"package": problemPackage},
		},
		"output-options": map[string]any{
			// Inline, so the config does not depend on where the framework
			// module sits on disk.
			"user-templates": oapitemplates.UserTemplates(),
		},
	}
}

// ClientCodegenYAML returns a minimal oapi-codegen client config.
func (c Config) ClientCodegenYAML() map[string]any {
	return map[string]any{
		"package": c.ClientPackage,
		"output":  c.ClientOutput,
		"generate": map[string]bool{
			"models": true,
			"client": true,
		},
	}
}

// DefaultServiceConfig returns the Nanostack service generation convention.
func DefaultServiceConfig(serverPackage string) Config {
	return Config{
		SpecPath:      "cmd/http/openapi.yaml",
		ServerPackage: serverPackage,
		ServerOutput:  "internal/apigen/gen.go",
		ClientPackage: "client",
		ClientOutput:  "clients/go/client.gen.go",
		TypesOutput:   "clients/types/index.ts",
		CleanSpecPath: "cmd/http/openapi.client.yaml",
	}
}
