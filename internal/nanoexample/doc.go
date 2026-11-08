// Package nanoexample wires every nano module together, as a service does. It
// is the in-repository check that the modules' providers bind into one
// generated graph and that their configs bind into one service Config: CI runs
// `nano gen wire -check` and `nano gen config -check` on it, and its tests
// bind the Config and build the graph.
//
// Regenerate with go generate ./internal/nanoexample after a module changes a
// provider or a config field. nano lives in the gen module, which requires this
// module, so the root module cannot pin it as a tool: the lines run it from the
// gen directory (go -C) and pass this directory back as -dir.
package nanoexample

//go:generate go -C ../../gen run ./cmd/nano gen config -dir ../internal/nanoexample
//go:generate go -C ../../gen run ./cmd/nano gen wire -dir ../internal/nanoexample -out .
