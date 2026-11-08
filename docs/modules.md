# Nano modules

A nano module is the generated-wiring replacement of an fx module in `modules/`. This guide lists the modules, the contract they share, and how a service moves from fx to them one module at a time.

## The modules

| Nano module | Replaces | Providers | Lifecycle | Config (section the service picks) |
| --- | --- | --- | --- | --- |
| [`core/logging`](../core/logging/README.md) | `modules/logging` | `NewLogger(log.Settings, []log.ContextAttrs) (*slog.Logger, error)`; declares `nano.Collect[log.ContextAttrs]()` | none; installs the slog default | `log.Settings`: `LOG_LEVEL`, `LOG_FORMAT`, `LOG_WIRE`, `ENVIRONMENT`, `SERVICE_NAME`, `SERVICE_VERSION` (fixed names) |
| [`otel/nanotrace`](../otel/nanotrace/README.md) | `modules/tracing` | `NewProvider(ctx, Config, log.Settings, *slog.Logger) (*Provider, error)`, `TracerProvider(*Provider) trace.TracerProvider`; member `ContextAttrs` of `Collect[log.ContextAttrs]` | `Starter` (installs the global provider and propagator), `Stopper` (flush) | `OTEL_SDK_DISABLED`, `OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_TRACES_SAMPLER_ARG` (fixed names) |
| [`pg/nanopg`](../pg/nanopg/README.md) | `modules/postgres`, `modules/transactor` | `NewDB(ctx, Config, *slog.Logger, trace.TracerProvider) (*DB, error)`, `SQL(*DB) *sql.DB`, `NewTransactor(*sql.DB) transactor.Transactor` | `*DB`: `Stopper` (close), `HealthChecker` (ping) | `postgres`: `host`, `port` (5432), `user`, `password` (secret), `db`, `sslmode`, `max_open_conns` (25), `max_idle_conns` (25), `conn_max_lifetime` (5m) |
| [`pg/nanomigrate`](../pg/nanomigrate/README.md) | `modules/migrations` | `NewMigrator(Config, *sql.DB, *slog.Logger) *Migrator`; `nano.Invoke(Run)` | none: `Run` is an Invoke | `migrations`: `enabled` (true), `base_path` (migrations), `validate` (true), `auto_fix_dirty` (false), `force_version` |
| [`pg/nanolock`](../pg/nanolock/README.md) | `modules/pglock` | `NewClient(ctx, Config, *sql.DB, *slog.Logger) (*pglock.Client, error)` | none | `pglock`: `table_name` (pglock_locks), `auto_create_table` (true), `lease_duration` (10s), `heartbeat_frequency` (1s), `test_on_startup` (true) |
| [`redis/nanocache`](../redis/nanocache/README.md) | `modules/cache` | `NewStore(ctx, Config, *slog.Logger) Store`; `New[T](Store, prefix, ttl, *slog.Logger) Cache[T]` | Redis store: `Stopper` (close), `HealthChecker` (ping) | `redis` for echopoint: `address`, `password` (secret), `db` |
| [`http/nanoserver`](../http/nanoserver/README.md) | server part of `modules/httpserver` | `NewServer(Config, http.Handler, *slog.Logger) *Server`; `NewReadiness(*nano.App) Readiness` for `/health` | `Runner` (listen, then drain) | `http`: `host`, `port` (8080), `read_header_timeout` (30s), `read_timeout` (60s), `write_timeout` (60s), `idle_timeout` (120s), `shutdown_timeout` (5s) |
| [`http/nanoapisec`](../http/nanoapisec/README.md) | `modules/apisec` | `NewResolver(Document) (*apisec.Resolver, error)`; the service provides `Document` (its embedded spec) | none | none |
| [`http/nanopprof`](../http/nanopprof/README.md) | `modules/pprof` | `NewServer(Config, *slog.Logger) *Server` | `Runner` (serves only when enabled) | `pprof`: `enabled` (env `ENABLE_PPROF`, false), `addr` (127.0.0.1:6060), the timeouts of `nanoserver` |
| [`modules/nanofx`](../modules/nanofx) | none: the bridge | `Lifecycle[T](name) fx.Option` | registers the nano lifecycle of `T` in fx | none |

No nano module replaces these fx modules:

- `modules/config`: the service binds its own `Config` with `nano gen config` (see [generation](generation.md#configuration-nano-gen-config)). Each module's `Config` is a field of it.
- `modules/pgqueue`: pgkit owns the queue lifecycle (see [its README](../modules/pgqueue/README.md)).

## The module contract

Every nano module has the same shape, so a service migrates each one the same way.

1. **Config.** A `Config` struct in the module package. Every field has a `config:` tag, a `default:` tag where the fx module had a default, and a `yaml:` tag with the key of the fx module (`dbname`, `tableName`, `basePath`, `autoFixDirty`, ...). Secrets are `secret.Secret`.
2. **No fixed env names**, except process-global ones (`LOG_*`, `OTEL_*`, `ENABLE_PPROF`). The service chooses the section, and `core/config` derives the env name from the path: section `postgres` and field `db` give `POSTGRES_DB`. A fixed name in a library would collide when a service has two sections of one type.
3. **Providers** are plain functions `func(deps...) T` or `func(deps...) (T, error)`. They take `*slog.Logger`, never zerolog. They do not read `os.Getenv`.
4. **Lifecycle** comes from the method set of the provided type: `Start`, `Stop`, `Run`, `CheckHealth` (see `core/nano`). A type outside the framework, such as a pgkit worker, has the same lifecycle when it has the methods.
5. **`var Module = nano.Module(...)`** in the package. The service's manifest lists it.

## Moving a service from fx, one module at a time

1. **Typed configuration first.** Declare the service `Config` with a field per module, generate `Bind` with `nano gen config`, and call it in `main`. Keep `runtimeenv.HydrateFileBackedEnv()` before `Bind` for `*_FILE` secrets.

   ```go
   type Config struct {
   	App      nano.AppConfig `config:"app"`
   	Log      log.Settings   `config:"log"`
   	Postgres nanopg.Config  `config:"postgres"`
   	Redis    nanocache.Config `config:"redis"`
   }
   ```

2. **Swap one fx module.** Supply the typed config, provide the nano constructors and register the lifecycle with `modules/nanofx`. For PostgreSQL, this replaces `postgres.Module` and `transactor.Module`:

   ```go
   fx.Supply(cfg.Postgres),
   fx.Provide(nanopg.NewDB, nanopg.SQL, nanopg.NewTransactor),
   nanofx.Lifecycle[*nanopg.DB]("postgres"),
   ```

   The nano constructors take a `*slog.Logger` and a `trace.TracerProvider` from the graph. Provide them once (`fx.Provide(func() *slog.Logger { ... })`), or move logging and tracing first.

   For migrations, call `nanomigrate.Run` from an `fx.Invoke`, as `modules/migrations` does: `fx.Invoke(func(m *nanomigrate.Migrator) error { return nanomigrate.Run(signalCtx, m) })`. Use a context that a SIGTERM cancels, so a stop during a migration ends clean.

3. **Repeat** for each module. Run the golden HTTP suite before and after each step.
4. **Replace `fx.New`** with a manifest and `nano gen wire` when every module is nano-shaped (see [generation](generation.md#wiring-nano-gen-wire)). `nano.Main` replaces the service's signal handling and its `Fatal` calls.

A service that moves its logging to `core/logging` gets the slog default, and the standard library `log` package then writes JSON through it. Set `LOG_WIRE=legacy` (or `wire: legacy` in the committed YAML layer) until the collector and the alerts read the slog shape.

### `/health`

`/health` stays the Docker `HEALTHCHECK` and a gate contract in the beta. The service keeps the `pkg/health` handler on its router, and registers the App's readiness in its registry:

```go
registry := health.NewRegistry()
registry.Register(nanoserver.NewReadiness(app))
mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Version: version, Registry: registry}))
```

The body keeps `status`, `service`, `environment`, `version`, `commit_sha` and `build_date`. It has `"readiness": "HEALTHY"` instead of the per-dependency keys, such as `"postgres"`, of the fx modules. While the App starts or stops, it answers 503.

## Differences from the fx modules

Each service migration PR lists the rows that apply to it.

| Module | Difference | Why |
| --- | --- | --- |
| all | Log keys are snake case: `base_path`, `up_file`, `missing_down` instead of `basePath`, `upFile`, `missingDown` | The slog key rule of the shared lint config. No alert reads these keys. |
| all | An invalid setting, such as `ENABLE_PPROF=yes` or `OTEL_TRACES_SAMPLER_ARG=2`, fails `Bind`. The fx modules fell back to a default | A typo must not change behaviour without notice. |
| `nanotrace` | The global provider and propagator are installed in `Start`, not at construction | `Start` makes the App build the provider when nothing takes it. Instrumentation created earlier delegates to the global provider. |
| `nanotrace` | The exporter endpoint must come from the environment, not from YAML | The OTLP exporter reads its own `OTEL_*` variables. The config fields only decide whether to export. |
| `nanopg` | Every DSN value is quoted, so a password with spaces or quotes connects. An empty `sslmode` is left out (lib/pq default) | The fx DSN broke on such a password. |
| `nanopg` | The pool sizes and lifetime are settings (same defaults) | They were constants. |
| `nanomigrate` | A stop during migrations comes from the context of `nano.Main`, not from a signal handler of the migrator | One owner for the signals. The migration in progress still finishes. |
| `nanomigrate` | A directory without migration files on a database without a version succeeds | The fx module failed with `first .: file does not exist`, against its own intent. |
| `nanocache` | `EvictPattern` walks keys with `SCAN` | `KEYS` blocks Redis on a large keyspace. |
| `nanocache` | `nanocache.ErrCacheKeyNotFound` is a different value from `cache.ErrCacheKeyNotFound` | The fx package cannot alias the nano one in the beta. Do not mix the stores of the two packages. |
| `nanoserver` | A listen failure stops the App | The fx module logged the failure and kept a process that took no traffic. |
| `nanoserver` | No CORS, OpenAPI validator, `/openapi.yaml` or tracing middleware | The service owns its router and middleware order. |
| `nanopprof` | A listen failure stops the App | The same reason as `nanoserver`. |
