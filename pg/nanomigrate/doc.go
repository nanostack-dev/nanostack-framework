// Package nanomigrate is the nano module that applies golang-migrate SQL
// migrations to PostgreSQL before the App starts.
//
// It replaces the deprecated fx module modules/migrations. A service lists
// Module in its manifest, next to pg/nanopg.Module, and puts Config in its own
// Config:
//
//	type Config struct {
//		Migrations nanomigrate.Config `config:"migrations"`
//	}
//
// Run is a nano.Invoke, not a Starter, on purpose. The generated Build calls
// it after construction and before App.Start, with the context of nano.Main,
// which has no deadline. A Starter would be bounded by AppConfig.StartTimeout
// (15s by default): a long migration would fail the start halfway, and the
// orchestrator would then roll the deployment back. A SIGINT or SIGTERM during
// Run cancels that context: the migration in progress finishes, no later one
// starts, and schema_migrations stays clean.
package nanomigrate
