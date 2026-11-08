// Package nanolock is the nano module that provides a cirello.io/pglock
// client: distributed locks leased in a PostgreSQL table.
//
// It replaces the deprecated fx module modules/pglock. A service lists Module
// in its manifest, next to pg/nanopg.Module, and puts Config in its own
// Config:
//
//	type Config struct {
//		PGLock nanolock.Config `config:"pglock"`
//	}
//
// The client needs no lifecycle: each lock stops its own heartbeat when it is
// released, and the pool belongs to pg/nanopg.
package nanolock
