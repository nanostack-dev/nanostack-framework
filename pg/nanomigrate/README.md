# pg/nanomigrate

The nano module that applies golang-migrate SQL migrations. It replaces the fx module `modules/migrations`.

- **Provides:** `*Migrator` from `NewMigrator(Config, *sql.DB, *slog.Logger)`, and runs `Run(ctx, *Migrator)` with `nano.Invoke`.
- **Config** (section `migrations`): `enabled` (true), `base_path` (yaml `basePath`, default `migrations`), `validate` (true), `auto_fix_dirty` (yaml `autoFixDirty`, false), `force_version` (yaml `forceVersion`).
- **Table:** `schema_migrations`, the same as the fx module.

`Run` is an Invoke, not a `Starter`. The generated `Build` calls it after construction and before `App.Start`, with the context of `nano.Main`, which has no deadline. A `Starter` would be bounded by `AppConfig.StartTimeout` (15s), and a long migration would fail the start. When SIGINT or SIGTERM cancels that context, the migration in progress finishes, no later migration starts, `Run` returns `ErrStopped`, and `schema_migrations` stays clean. A migration that fails during the stop is returned as that failure, not as `ErrStopped` (`schema_migrations` can be dirty), and a run that finished its last migration before the stop took effect returns nil.

`Migrate` runs on one connection that it takes from the `*sql.DB` pool and gives back at the end. It never closes the pool.

`Validate` logs a warning for each `<base>/<module>/migrations/*.up.sql` file without its down file. `Config.Validate: false` turns it off.

Run the integration tests with Docker: `go test -tags=integration ./pg/nanomigrate/`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
