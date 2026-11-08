// Package example is a small webhook API served by the generated std-http
// strict server, with the nanohttp template override and nano gen validate
// validators. Its tests exercise the HTTP boundary end to end.
package example

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
//go:generate go -C ../../../../gen run ./cmd/nano gen validate -spec ../http/nanohttp/internal/example/openapi.yaml -dir ../http/nanohttp/internal/example
