// Package nanoexample wires every nano module together, as a service does. It
// is the in-repository check that the modules' providers bind into one
// generated graph and that their configs bind into one service Config: CI runs
// `nano gen wire -check` and `nano gen config -check` on it, and its tests
// bind the Config and build the graph.
//
// Regenerate with go generate ./internal/nanoexample after a module changes a
// provider or a config field.
package nanoexample

//go:generate go run github.com/nanostack-dev/nanostack-framework/cmd/nano gen config -dir .
//go:generate go run github.com/nanostack-dev/nanostack-framework/cmd/nano gen wire -dir . -out .
