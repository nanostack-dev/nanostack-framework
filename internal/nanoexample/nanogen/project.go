// Package nanogen declares the inputs of nanoexample's code generation that
// nano gen cannot discover. nanoexample has none: nano gen finds the
// manifest and Config by parsing, and writes wire.gen.go, config.gen.go,
// CONFIG.md and .env.example next to them. The empty declaration is the
// smallest one a module can have. Run from the repository root:
//
//	go -C gen run ./cmd/nano gen -dir ../internal/nanoexample
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

// Project is read from source by nano gen; nothing calls it.
var Project = project.Project{} //nolint:gochecknoglobals // a declaration nano gen reads, like nano.Manifest
