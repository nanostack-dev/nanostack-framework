// Package nullabletype is the golden package for nullable members under
// oapi-codegen's nullable-type option, where absent, null and a value are
// three states of nullable.Nullable.
package nullabletype

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go run github.com/nanostack-dev/nanostack-framework/gen/cmd/nanostack gen validate -spec openapi.yaml -dir .
