// Package unions is the golden package for oneOf with a discriminator, oneOf and anyOf without one.
package unions

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go run github.com/nanostack-dev/nanostack-framework/gen/cmd/nanostack gen validate -spec openapi.yaml -dir .
