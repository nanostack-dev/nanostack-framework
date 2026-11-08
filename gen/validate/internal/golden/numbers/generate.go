// Package numbers is the golden package for minimum, maximum, exclusive bounds (3.0) and multipleOf.
package numbers

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go run github.com/nanostack-dev/nanostack-framework/gen/cmd/nano gen validate -spec openapi.yaml -dir .
