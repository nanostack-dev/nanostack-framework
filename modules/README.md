# modules

Fx lifecycle wiring for the legacy service stack.

Every fx module in this tree is deprecated and is removed with fx (ADR 0013). Each one keeps working, with its fx signatures, through the beta, so a service on v0.22.0 upgrades with no change to its graph. New code uses the nano module that replaces it:

| fx module | Nano module |
| --- | --- |
| `modules/logging` | `core/logging` |
| `modules/tracing` | `otel/nanotrace` |
| `modules/postgres`, `modules/transactor` | `pg/nanopg` |
| `modules/migrations` | `pg/nanomigrate` |
| `modules/pglock` | `pg/nanolock` |
| `modules/cache` | `redis/nanocache` |
| `modules/httpserver` | `http/nanoserver` |
| `modules/pprof` | `http/nanopprof` |
| `modules/config` | `core/config` and `nano gen config` |
| `modules/apisec` | `http/nanoapisec` (the fx module wraps it) |
| `modules/pgqueue` | pgkit (see its README) |

`modules/nanofx` runs a nano component in an fx graph, so a service moves one module at a time. [docs/modules.md](../docs/modules.md) has the migration steps and the differences of each module.
