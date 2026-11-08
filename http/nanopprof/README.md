# http/nanopprof

The nano module that serves `net/http/pprof` on a separate listener. It replaces the fx module `modules/pprof`.

- **Provides:** `*Server` from `NewServer(Config, *slog.Logger)`.
- **Lifecycle:** `Server` is a `Runner`. When enabled, it serves the profiles until the App stops. When disabled, it serves nothing and waits for the App to stop, because a `Runner` that returns early stops the App. A listen failure stops the App. A profile still running after `shutdown_timeout` is cut, logged at Warn, and does not fail the stop.
- **Config** (section `pprof`): `enabled` (env `ENABLE_PPROF` whatever the section, default false), `addr` (127.0.0.1:6060), and the timeouts of `http/nanoserver`. A CPU profile of N seconds needs a `write_timeout` above N. An empty `addr` or a zero timeout in a `Config` built by hand takes its default; `DefaultConfig()` returns them.

Keep `addr` on loopback: profiles show memory contents. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
