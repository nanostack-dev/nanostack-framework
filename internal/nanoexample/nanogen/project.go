// Package nanogen declares the inputs of nanoexample's code generation that
// nanostack gen cannot discover. nanoexample has none: nanostack gen finds the
// manifest and Config by parsing, and writes wire.gen.go, config.gen.go,
// CONFIG.md and .env.example next to them. The empty declaration is the
// smallest one a module can have. Run from the repository root:
//
//	go -C gen run ./cmd/nanostack gen -dir ../internal/nanoexample
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

// Project is read from source by nanostack gen; nothing calls it.
var Project = project.Project{} //nolint:gochecknoglobals // a declaration nanostack gen reads, like nano.Manifest
