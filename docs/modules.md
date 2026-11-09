# Nano modules

A nano module is the generated-wiring replacement of an fx module in `modules/`. This guide lists the modules, the contract they share, and how a service moves from fx to them one module at a time.

## The modules

| Nano module | Replaces | Providers | Lifecycle | Config (section the service picks) |
| --- | --- | --- | --- | --- |
| [`core/logging`](../core/logging/README.md) | `modules/logging` | `NewLogger(log.Settings, []log.ContextAttrs) (*slog.Logger, error)`; declares `nano.Collect[log.ContextAttrs]()` | none; installs the slog default | `log.Settings`: `LOG_LEVEL`, `LOG_FORMAT`, `LOG_WIRE`, `ENVIRONMENT`, `SERVICE_NAME`, `SERVICE_VERSION` (fixed names) |
| [`otel/nanotrace`](../otel/nanotrace/README.md) | `modules/tracing` | `NewProvider(ctx, Config, log.Settings, *slog.Logger) (*Provider, error)`, `TracerProvider(*Provider) trace.TracerProvider`, `NewInstaller(*Provider) *Installer`; member `ContextAttrs` of `Collect[log.ContextAttrs]` | `*Installer`: `Starter` (installs the global provider and propagator); `*Provider`: `Stopper` (flush), run even when the App never starts | `OTEL_SDK_DISABLED`, `OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_TRACES_SAMPLER_ARG` (fixed names) |
| [`pg/nanopg`](../pg/nanopg/README.md) | `modules/postgres`, `modules/transactor` | `NewDB(ctx, Config, *slog.Logger, trace.TracerProvider) (*DB, error)`, `SQL(*DB) *sql.DB`, `NewTransactor(*sql.DB) transactor.Transactor` | `*DB`: `Stopper` (close), `HealthChecker` (ping) | `postgres`: `host`, `port` (5432), `user`, `password` (secret), `db`, `sslmode`, `max_open_conns` (25), `max_idle_conns` (25), `conn_max_lifetime` (5m) |
| [`pg/nanomigrate`](../pg/nanomigrate/README.md) | `modules/migrations` | `NewMigrator(Config, *sql.DB, *slog.Logger) *Migrator`; `nano.Invoke(Run)` | none: `Run` is an Invoke | `migrations`: `enabled` (true), `base_path` (migrations), `validate` (true), `auto_fix_dirty` (false), `force_version` |
| [`pg/nanolock`](../pg/nanolock/README.md) | `modules/pglock` | `NewClient(ctx, Config, *sql.DB, *slog.Logger) (*pglock.Client, error)` | none | `pglock`: `table_name` (pglock_locks), `auto_create_table` (true), `lease_duration` (10s), `heartbeat_frequency` (1s), `test_on_startup` (true) |
| [`redis/nanocache`](../redis/nanocache/README.md) | `modules/cache` | `NewStore(ctx, Config, *slog.Logger) Store`; `New[T](Store, prefix, ttl, *slog.Logger) (Cache[T], error)` | Redis store: `Stopper` (close), `HealthChecker` (ping) | `redis` for echopoint: `address`, `password` (secret), `db` |
| [`http/nanoserver`](../http/nanoserver/README.md) | server part of `modules/httpserver` | `NewServer(Config, http.Handler, *slog.Logger) *Server`; `NewReadiness(*nano.App) Readiness` or `ReadinessMiddleware(*nano.App)` for `/health` | `Runner` (listen, then drain) | `http`: `host`, `port` (8080), `read_header_timeout` (30s), `read_timeout` (60s), `write_timeout` (60s), `idle_timeout` (120s), `shutdown_timeout` (5s) |
| [`http/nanoapisec`](../http/nanoapisec/README.md) | `modules/apisec`: list `ChiModule` | `ChiModule`: `NewChiResolver(Document) (*apisec.Resolver, error)`, matched as chi, as `modules/apisec` did. `Module`: `NewResolver(Document)`, matched as `http.ServeMux` and `nanohttp.NewRouter`. The service provides `Document` (its embedded spec) | none | none |
| [`http/nanopprof`](../http/nanopprof/README.md) | `modules/pprof` | `NewServer(Config, *slog.Logger) *Server` | `Runner` (serves only when enabled) | `pprof`: `enabled` (env `ENABLE_PPROF`, false), `addr` (127.0.0.1:6060), the timeouts of `nanoserver` |
| [`modules/nanofx`](../modules/nanofx) | none: the bridge | `Lifecycle[T](name) fx.Option` | registers the nano lifecycle of `T` in fx | none |

No nano module replaces these fx modules:

- `modules/config`: the service binds its own `Config` with `nanostack gen config` (see [generation](generation.md#configuration-nanostack-gen-config)). Each module's `Config` is a field of it.
- `modules/pgqueue`: pgkit owns the queue lifecycle (see [its README](../modules/pgqueue/README.md)).

## The module contract

Every nano module has the same shape, so a service migrates each one the same way.

1. **Config.** A `Config` struct in the module package. Every field has a `config:` tag, and a `default:` tag where the fx module had a default. `core/config` and the generated `Bind` read only the `config:` names. A `yaml:` tag keeps the legacy key of the fx module (`dbname`, `tableName`, `basePath`, `autoFixDirty`, ...) for the deprecated fx loader only: a YAML layer written for the fx module does not bind to a nano module, so rename its keys when the service moves to `Bind` (see the key-rename row below). Secrets are `secret.Secret`. Outside `Bind`, the constructor gives a zero duration or size its default, and `DefaultConfig()` returns the defaults of the tags (a test keeps it equal to them with `core/config/configtest`). A `Config` built by hand, in a test or an `Override`, starts from `DefaultConfig()`, so the defaults for which zero is a valid value (`Enabled`, `SampleRatio`, `MaxIdleConns`, a port) apply too.
2. **No fixed env names**, except process-global ones (`LOG_*`, `OTEL_*`, `ENABLE_PPROF`). The service chooses the section, and `core/config` derives the env name from the path: section `postgres` and field `db` give `POSTGRES_DB`. A fixed name in a library would collide when a service has two sections of one type.
3. **Providers** are plain functions `func(deps...) T` or `func(deps...) (T, error)`. They take `*slog.Logger`, never zerolog. They do not read `os.Getenv`. They do not read the service's tables either: the migrations run after every constructor (see [Construction order](#construction-order)).
4. **Lifecycle** comes from the method set of the provided type: `Start`, `Stop`, `Run`, `CheckHealth` (see `core/nano`). A type outside the framework, such as a pgkit worker, has the same lifecycle when it has the methods.
5. **`var Module = nano.Module(...)`** in the package. The service's manifest lists it.

## Moving a service from fx, one module at a time

1. **Typed configuration first.** Declare the service `Config` with a field per module, generate `Bind` with `nanostack gen config`, and call it in `main`. For `*_FILE` secrets, keep the service's own `runtimeenv.HydrateFileBackedEnv()` before `Bind` (echopoint and anchor code, not a framework package), or pass a secret store to the sources: `config.Secrets(config.SecretDir(dir))` reads one file per secret from a mounted directory.

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
   nanofx.Context(signalCtx),
   fx.Supply(cfg.Postgres),
   fx.Provide(nanopg.NewDB, nanopg.SQL, nanopg.NewTransactor),
   nanofx.Lifecycle[*nanopg.DB]("postgres"),
   ```

   The constructors that dial a server (`nanopg.NewDB`, `nanocache.NewStore`, `nanolock.NewClient`) take a `context.Context` first, and an fx graph has none: `nanofx.Context` provides it once. Pass the context that SIGTERM cancels. The nano constructors take a `*slog.Logger` and a `trace.TracerProvider` from the graph. Provide them once (`fx.Provide(func() *slog.Logger { ... })`), or move logging and tracing first.

   For tracing, provide `nanotrace.NewInstaller` too and register both `nanofx.Lifecycle[*nanotrace.Provider]` (flush) and `nanofx.Lifecycle[*nanotrace.Installer]` (installs the global provider). Without the Installer, nothing installs the provider and no error says so.

   For migrations, call `nanomigrate.Run` from an `fx.Invoke`, as `modules/migrations` does: `fx.Invoke(func(m *nanomigrate.Migrator) error { return nanomigrate.Run(signalCtx, m) })`. Use a context that a SIGTERM cancels, so a stop during a migration ends clean.

3. **Repeat** for each module. Run the golden HTTP suite before and after each step.
4. **Replace `fx.New`** with a manifest and `nanostack gen wire` when every module is nano-shaped (see [generation](generation.md#wiring-nanostack-gen-wire)). `nano.Main` replaces the service's signal handling and its `Fatal` calls. Before this step, move every database read out of the constructors (see [Construction order](#construction-order)).

A service that moves its logging to `core/logging` gets the slog default, and the standard library `log` package then writes JSON through it. Set `LOG_WIRE=legacy` (or `wire: legacy` in the committed YAML layer) until the collector and the alerts read the slog shape.

### Construction order

The generated `Build` works in three phases:

1. It calls every provider of the graph, in dependency order: each value that a `Starter`, a `Runner` or a `nano.Invoke` needs, and each `Starter` and `Runner` (see [generation](generation.md#wiring-nanostack-gen-wire)).
2. It runs the `nano.Invoke` functions in declaration order. `nanomigrate.Module` declares `nano.Invoke(nanomigrate.Run)`, so the migrations run here.
3. `nano.Main` then starts the App: each `Starter`'s `Start` runs in dependency order, then the `Runner`s.

fx built a value when an `fx.Invoke` first needed it, and it ran the invokes in registration order. `modules/migrations` registers its invoke early, so the migrations usually ran before the constructors of the features. Under `Build`, every constructor runs before the migrations. A constructor that reads the service's own tables (a schema check, a cache warm-up, a sync of reference data) then fails on a fresh database, where the tables do not exist yet.

Move the read out of the constructor:

- **A `Starter`.** Give the component a `Start(ctx) error` method that does the read. `Start` runs after `Build`, so after the migrations, under `AppConfig.StartTimeout`. Use it for a short read. Anchor's `SchemaCheck`, an fx invoke before, is a `Starter` now:

  ```go
  func NewSchemaCheck(db *sql.DB) *SchemaCheck { return &SchemaCheck{db: db} }

  func (c *SchemaCheck) Start(ctx context.Context) error { return CheckSchema(ctx, c.db) }
  ```

- **A `nano.Invoke` declared after `nanomigrate.Module`.** Invokes run in declaration order, so list the module that declares it after `nanomigrate.Module` in the manifest. An Invoke has no deadline. Use it for work that can outlast `StartTimeout`.

### `/health`

`/health` stays the Docker `HEALTHCHECK` and a gate contract in the beta. The service keeps the `pkg/health` handler on its router, and registers the App's readiness in its registry:

```go
registry := health.NewRegistry()
registry.Register(nanoserver.NewReadiness(app))
mux.Handle("GET /health", health.NewHandler(health.Config{Service: "billing", Version: version, Registry: registry}))
```

The body keeps `status`, `service`, `environment`, `version`, `commit_sha` and `build_date`. It has `"readiness": "HEALTHY"` instead of the per-dependency keys, such as `"postgres"`, of the fx modules. While the App starts or stops, it answers 503.

A service whose `/health` body is pinned, such as by a gate flow, wraps its handler in `nanoserver.ReadinessMiddleware` instead. The middleware sets the status and leaves the body byte for byte the same:

```go
router.With(nanoserver.ReadinessMiddleware(app)).Get("/health", healthHandler)
```

While the App runs and every `HealthChecker` passes, the response is the handler's own. While the App starts or drains, or a check fails, a 2xx or 3xx status, or no status at all, becomes 503. An error status of the handler stays. The body still says `"status": "HEALTHY"`: the status code is the readiness signal. The Docker `HEALTHCHECK` (`health.Probe`) and the load balancer read only the code.

A service that mounts the handler with `health.Mount` passes the wrapped chi router: `health.Mount(router.With(nanoserver.ReadinessMiddleware(app)), cfg)`. Only `GET /health` is wrapped, so other methods keep their 405.

## Differences from the fx modules

Each service migration PR lists the rows that apply to it.

| Module | Difference | Why |
| --- | --- | --- |
| all | Log keys are snake case: `base_path`, `up_file`, `missing_down` instead of `basePath`, `upFile`, `missingDown` | The slog key rule of the shared lint config. No alert reads these keys. |
| all | An invalid setting, such as `ENABLE_PPROF=yes` or `OTEL_TRACES_SAMPLER_ARG=2`, fails `Bind`. The fx modules fell back to a default | A typo must not change behaviour without notice. |
| all | A secret file that `config.SecretDir` reads keeps its whitespace, except one line ending at the end (`\n`, `\r\n` or `\r`). The fx loader's `${file:VAR}` trimmed all leading and trailing whitespace. The framework reads no `*_FILE` variable: the service's own hydration does (echopoint's and anchor's `runtimeenv.HydrateFileBackedEnv` trims every trailing `\r` and `\n`) | A secret is the file's bytes. Write the file without padding. |
| all | No module loads a `.env` file. The fx loader read `./.env` (or `ENV_FILE`) before it bound the config | The environment comes from the process. A local run exports the file itself, for example `set -a; . ./.env; set +a`. |
| all | A YAML layer uses the `config:` names in snake case (`db`, `table_name`, `base_path`, `auto_fix_dirty`), not the fx keys (`dbname`, `tableName`, `basePath`, `autoFixDirty`). `core/config` does not expand `${VAR}` in a value. A secret setting (a password) in a file layer is an issue, not a value: it comes from the environment, a `*_FILE` variable or the secret store | `Bind` reads the `config:` tags only. Config files are committed, so they hold no secrets and no indirection. |
| `nanotrace` | The global provider and propagator are installed in `Installer.Start`, not at construction | A `Starter` makes the App build the provider when nothing takes it. Instrumentation created earlier delegates to the global provider. The flush is the `Provider`'s start-less `Stop`, so a build that fails after it still exports the spans of the failure. |
| `nanotrace` | The endpoint may come from a YAML layer. An endpoint that is not an http or https URL fails the build | The bound `endpoint` and `traces_endpoint` reach the exporter, with the specification rule (`/v1/traces` joined to the base endpoint). Headers and timeout still come from `OTEL_*`. |
| `nanopg` | Every DSN value is quoted, so a password with spaces or quotes connects. An empty `sslmode` is left out, and lib/pq then uses `PGSSLMODE` or, without it, `require`. The fx DSN wrote `sslmode=`, which lib/pq v1.12 refuses (`"" is not supported`), so the fx module did not connect. A database without TLS needs `sslmode: disable` | The fx DSN broke on such a password and on an empty `sslmode`. |
| `nanopg` | The pool sizes and lifetime are settings (same defaults) | They were constants. |
| `nanomigrate` | `Run` is an Invoke that runs after every constructor, not before the constructors of the features | See [Construction order](#construction-order). A constructor that reads the service's tables fails on a fresh database. |
| `nanomigrate` | A stop during migrations comes from the context of `nano.Main`, not from a signal handler of the migrator | One owner for the signals. The migration in progress still finishes. |
| `nanomigrate` | A stop while another instance holds the migration lock returns `ErrStopped` at once | golang-migrate waits for the lock with `context.Background`, so the stop waited for the other migration, or for SIGKILL. |
| `nanomigrate` | A directory without migration files on a database without a version succeeds | The fx module failed with `first .: file does not exist`, against its own intent. |
| `nanocache` | `EvictPattern` walks keys with `SCAN` and unlinks each page | `KEYS` blocks Redis on a large keyspace, and one `DEL` of every match is unbounded. |
| `nanocache` | A key part's `:` and `\` are escaped (`a:b` is stored as `a\:b`), and `EvictPrefix` escapes the glob characters | A free-text part could collide with another key or evict another scope. Keys of fixed-format parts (UUIDs, hashes) do not change; other keys start cold after the deploy. |
| `nanocache` | `nanocache.ErrCacheKeyNotFound` is a different value from `cache.ErrCacheKeyNotFound` | The fx package cannot alias the nano one in the beta. Do not mix the stores of the two packages. |
| `nanoapisec` | `Module` matches a request as `http.ServeMux` does. `modules/apisec` matched as chi, so its drop-in replacement is `ChiModule` | The two routers disagree on an escaped path (`/files/%61dmin`) and on sibling precedence. A resolver that disagrees with the router checks the wrong operation's security. |
| `nanoserver` | A listen failure stops the App | The fx module logged the failure and kept a process that took no traffic. |
| `nanoserver` | The process lines change: `http server started` carries `addr`, not `port`. The drain logs `http server draining` at Info, and `http server drain timed out; connections closed` at Warn when it cuts connections. `net/http`'s own errors (a TLS handshake, a broken header) are Warn lines through slog, not plain text on stderr | The lines go through the service's logger. Check the alerts and dashboards that read the old lines. |
| `nanoserver` | No CORS, OpenAPI validator, `/openapi.yaml` or tracing middleware | The service owns its router and middleware order. |
| `nanopprof` | A listen failure stops the App | The same reason as `nanoserver`. |
