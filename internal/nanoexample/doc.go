// Package nanoexample wires every nano module together, as a service does. It
// is the in-repository check that the modules' providers bind into one
// generated graph and that their configs bind into one service Config: CI runs
// `nanostack gen -check` on it, and its tests bind the Config and build the graph.
//
// Regenerate with the pipeline after a module changes a provider or a config
// field, from the repository root:
//
//	go -C gen run ./cmd/nanostack gen -dir ../internal/nanoexample
//
// nanostack lives in the gen module, which requires this module, so the root
// module cannot pin it as a tool: the command runs it from the gen directory
// (go -C) and passes this directory back as -dir. nanogen/project.go is the
// project declaration; CI runs the same command with -check.
package nanoexample
