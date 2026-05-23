package oapi

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

// ServerCodegenYAML returns a minimal oapi-codegen server config.
func (c Config) ServerCodegenYAML() map[string]any {
	return map[string]any{
		"package": c.ServerPackage,
		"output":  c.ServerOutput,
		"generate": map[string]bool{
			"models":        true,
			"chi-server":    true,
			"strict-server": true,
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
