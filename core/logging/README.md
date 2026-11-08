# core/logging

The nano module that builds the process logger. It replaces the fx module `modules/logging`.

- **Provides:** `*slog.Logger` from `NewLogger(log.Settings, []log.ContextAttrs)`. It also declares the `[]log.ContextAttrs` collection, so other modules add context attributes with `nano.Collect[log.ContextAttrs](fn)`.
- **Config:** `log.Settings` in the service `Config`. The env names are fixed, because one process has one log configuration: `LOG_LEVEL`, `LOG_FORMAT`, `LOG_WIRE`, `ENVIRONMENT`, `SERVICE_NAME`, `SERVICE_VERSION`.
- **Effect:** `NewLogger` installs the logger as the slog default. Code without an injected logger, such as `http/problem.Write`, and the standard library `log` package then write through it.
- **Errors:** an invalid `LOG_LEVEL`, `LOG_FORMAT` or `LOG_WIRE` fails the build.

Set `LOG_WIRE=legacy` while the collector and the alerts read the zerolog shape (see [core/log](../log/README.md)).

The module is a separate package from `core/log`, because `core/nano` imports `core/log`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
