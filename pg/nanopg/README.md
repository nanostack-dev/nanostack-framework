# pg/nanopg

The nano module for PostgreSQL. It replaces the fx modules `modules/postgres` and `modules/transactor`.

- **Provides:** `*DB` from `NewDB(ctx, Config, *slog.Logger, trace.TracerProvider)`, `*sql.DB` from `SQL(*DB)`, and `transactor.Transactor` from `NewTransactor(*sql.DB)`.
- **Lifecycle:** `*DB` is a `Stopper` (closes the pool) and a `HealthChecker` (pings, for `/readyz`).
- **Config** (section `postgres` gives `POSTGRES_*`): `host`, `port` (5432), `user`, `password` (secret, optional), `db` (env `POSTGRES_DB`; legacy fx key `dbname`), `sslmode` (empty: lib/pq default), `max_open_conns` (25), `max_idle_conns` (25), `conn_max_lifetime` (5m).
- **Start:** `NewDB` pings the server for at most 5 seconds. A failed ping fails the build.
- **Tracing:** a query is a child span only when its context has a span, with the options of `modules/postgres`. Provide `trace.TracerProvider` with `otel/nanotrace`.

Run the integration test with Docker: `go test -tags=integration ./pg/nanopg/` (or set `PG_TEST_DSN`). See [docs/modules.md](../../docs/modules.md) for the migration from fx.
