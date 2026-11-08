// Package example is a small webhook API served by the generated std-http
// strict server, with the nanohttp template override and nano gen validate
// validators. Its tests exercise the HTTP boundary end to end.
//
// nano gen generates api.gen.go, handlers.gen.go (the split of the server
// by OpenAPI tag: NotesServer, WebhooksServer and Handlers) and
// validate.gen.go from openapi.yaml, as nanogen/project.go declares. Run from the repository root:
//
//	go -C gen run ./cmd/nano gen -dir ../http/nanohttp/internal/example
package example
