// Package arrays is the golden package for minItems, maxItems, uniqueItems and items.
package arrays

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go run github.com/nanostack-dev/nanostack-framework/gen/cmd/nano gen validate -spec openapi.yaml -dir .
