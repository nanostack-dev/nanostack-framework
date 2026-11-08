// Package nanopg is the nano module for PostgreSQL. It opens a pooled,
// traced *sql.DB, closes it when the App stops, reports it to /readyz and
// provides the transactor of pkg/db/transactor on top of it.
//
// It replaces the deprecated fx modules modules/postgres and
// modules/transactor. A service lists Module in its manifest, with a
// trace.TracerProvider provider (otel/nanotrace.Module), and puts Config in
// its own Config:
//
//	type Config struct {
//		Postgres nanopg.Config `config:"postgres"`
//	}
//
// With the section postgres, the env names are POSTGRES_HOST, POSTGRES_PORT,
// POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB, POSTGRES_SSLMODE and the
// POSTGRES_* pool settings.
package nanopg
