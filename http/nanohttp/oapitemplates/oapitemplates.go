// Package oapitemplates holds the oapi-codegen user-template overrides that
// connect the generated std-http strict server to nanohttp and problem, and
// make union types decode as strictly as the request body.
//
// Use it from an oapi-codegen v2.8.2 config:
//
//	package: api
//	output: api.gen.go
//	generate:
//	  models: true
//	  std-http-server: true
//	  strict-server: true
//	additional-imports:
//	  - package: github.com/nanostack-dev/nanostack-framework/http/nanohttp
//	  - package: github.com/nanostack-dev/nanostack-framework/http/problem
//	output-options:
//	  user-templates:
//	    strict/strict-http.tmpl: <path to strict/strict-http.tmpl>
//	    union.tmpl: <path to union.tmpl>
//
// Then run nano gen validate on the same package: the overridden template
// calls the Validate and validateParams methods it generates, so the package
// does not compile until they exist and match the spec.
//
// union.tmpl makes the As<Variant> methods of a oneOf or anyOf type decode
// with nanohttp.Unmarshal, the strict json/v2 options the validators use.
// The upstream template decodes with encoding/json v1, which matches member
// names case-insensitively, so a handler could read a value that validation
// never checked.
//
// The overrides are pinned to oapi-codegen v2.8.2. Upgrading oapi-codegen
// means diffing upstream strict/strict-http.tmpl and union.tmpl against these
// copies.
package oapitemplates

import "embed"

// FS holds strict/strict-http.tmpl and union.tmpl.
//
//go:embed strict/strict-http.tmpl union.tmpl
var FS embed.FS

// OapiCodegenVersion is the oapi-codegen release the override was cut from.
const OapiCodegenVersion = "v2.8.2"

// UserTemplates returns the overrides as oapi-codegen's
// output-options.user-templates map, with the templates inline. Use it when
// driving oapi-codegen as a library.
func UserTemplates() map[string]string {
	templates := map[string]string{}
	for _, name := range []string{"strict/strict-http.tmpl", "union.tmpl"} {
		b, err := FS.ReadFile(name)
		if err != nil {
			panic("oapitemplates: embedded template missing: " + err.Error())
		}
		templates[name] = string(b)
	}
	return templates
}
