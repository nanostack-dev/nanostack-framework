# pg/nanomigrate

The nano module that applies golang-migrate SQL migrations. It replaces the fx module `modules/migrations`.

- **Provides:** `*Migrator` from `NewMigrator(Config, *sql.DB, *slog.Logger)`, and runs `Run(ctx, *Migrator)` with `nano.Invoke`.
- **Config** (section `migrations`): `enabled` (true), `base_path` (legacy fx key `basePath`, default `migrations`), `validate` (true), `auto_fix_dirty` (legacy fx key `autoFixDirty`, false), `force_version` (legacy fx key `forceVersion`).
- **Table:** `schema_migrations`, the same as the fx module.

`Run` is an Invoke, not a `Starter`. The generated `Build` calls it after construction and before `App.Start`, with the context of `nano.Main`, which has no deadline. A `Starter` would be bounded by `AppConfig.StartTimeout` (15s), and a long migration would fail the start. When SIGINT or SIGTERM cancels that context, the migration in progress finishes, no later migration starts, `Run` returns `ErrStopped`, and `schema_migrations` stays clean. A migration that fails during the stop is returned as that failure, not as `ErrStopped` (`schema_migrations` can be dirty), and a run that finished its last migration before the stop took effect returns nil.

Every constructor of the App runs before `Run`. A constructor that reads the service's own tables fails on a fresh database. Do that read in a `Starter`, or in a `nano.Invoke` declared after this module (see [Construction order](../../docs/modules.md#construction-order)).

`Migrate` runs on one connection that it takes from the `*sql.DB` pool and gives back at the end. It never closes the pool.

While another instance migrates, `Migrate` waits for the migration advisory lock (the one golang-migrate takes), polling with a backoff up to 2s. A stop during that wait returns `ErrStopped` at once; golang-migrate alone would wait for the other migration to end. A connection whose lock release cannot be confirmed is discarded, so a session that holds the lock never returns to the pool.

`Validate` logs a warning for each `<base>/<module>/migrations/*.up.sql` file without its down file. `Config.Validate: false` turns it off.

Run the integration tests with Docker: `go test -tags=integration ./pg/nanomigrate/`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
