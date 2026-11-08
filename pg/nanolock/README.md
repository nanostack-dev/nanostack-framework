# pg/nanolock

The nano module for distributed locks leased in a PostgreSQL table (`cirello.io/pglock`). It replaces the fx module `modules/pglock`.

- **Provides:** `*pglock.Client` from `NewClient(ctx, Config, *sql.DB, *slog.Logger)`.
- **Lifecycle:** none. Each lock stops its heartbeat when it is released, and the pool belongs to `pg/nanopg`.
- **Config** (section `pglock`), with the defaults of the fx module: `table_name` (pglock_locks), `auto_create_table` (true), `lease_duration` (10s), `heartbeat_frequency` (1s), `test_on_startup` (true). The lease must be at least two heartbeats. `NewClient` defaults a zero duration and rejects a negative one; a `Config` built by hand starts from `DefaultConfig()`.
- **Pool:** the `*sql.DB` of `pg/nanopg`, which is lib/pq wrapped by otelsql. `NewClient` uses `pglock.UnsafeNew` because `pglock.New` accepts only a bare `*pq.Driver` pool.
- **Start:** with `test_on_startup`, `NewClient` acquires and releases a random lock, so a missing table or permission fails the build.

Run the integration tests with Docker: `go test -tags=integration ./pg/nanolock/`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
