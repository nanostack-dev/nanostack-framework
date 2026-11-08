// Package numbers31 is the golden package for OpenAPI 3.1 numeric exclusive bounds and integer type arrays.
package numbers31

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go run github.com/nanostack-dev/nanostack-framework/cmd/nano gen validate -spec openapi.yaml -dir .
