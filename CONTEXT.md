# Nanostack Framework context

The framework owns reusable service infrastructure shared by Nanostack applications. It leaves product meaning and policies with each application.

## Language

**Framework package:** A narrowly scoped primitive usable independently of the framework's service assembly. Avoid calling an unbounded collection a toolkit or common bucket.

**Framework module:** A reusable lifecycle and dependency-injection integration that makes infrastructure available to an application. Today the word means the fx form in `modules/`, which is deprecated. Do not confuse it with a released module (a Go module) or a nano module.

**Nano module:** The generated-wiring form of a framework module: a package in a layered tree (`core/logging`, `pg/`, `redis/`, `otel/`, `http/nano*`) with a `Config`, plain provider functions, lifecycle methods (`Start`, `Stop`, `Run`, `CheckHealth`) and a `var Module = nano.Module(...)`. [docs/modules.md](docs/modules.md) lists them.

**fx bridge:** `modules/nanofx`. It registers the lifecycle of a nano module in an fx graph, so a service moves one module at a time.

**Readiness:** Whether the App is started and every `HealthChecker` passes, as `nano.App` evaluates it. `nanoserver.Readiness` reports it in the legacy `/health` body. Liveness (`/livez`) is a different check.

**Nanostack DB:** The `db` module: the framework's fork of go-jet with native `functional.Option` support. Say "Nanostack DB", not "go-jet", for the code in `db/`; [db/UPSTREAM.md](db/UPSTREAM.md) records what differs.

**Released module:** One of the three Go modules that this repository tags: the root module, `db` and `pkg/functional`. Each has its own version and its own tag prefix ([RELEASING.md](RELEASING.md)).

**Leaves first:** The release order of the released modules: a module is tagged only after every module of this repository that it requires: `pkg/functional`, then `db`, then the root.

**Service assembly:** The composition of framework modules and application-owned dependencies into one service.

**Application adapter:** Product-owned behavior supplied to a framework integration, such as credential verification or transport error handling.

**Generation configuration:** Explicit inputs and conventions for generating application server, client or database code; generated product contracts remain application-owned.

**Starter:** The proposed baseline for a new service. The current repository contains starter documentation rather than a complete scaffolding command.

**Problem:** Only an RFC 9457 `application/problem+json` object, built and written by `http/problem`. Do not call a config issue or a validation list a problem.

**Legacy error envelope:** The `{"errors":[{"code","message","metadata"}]}` body with `Content-Type: application/json` that echopoint and anchor answer today (`pkg/fault`). `pkg/httputil/errorenvelope` writes it on the nano HTTP stack. It is not a problem; it goes away when the services answer problems.

**Leaf layer:** The layer below `core/` in plan section 06: `pkg/functional`, its own module with no framework dependency. Every layer may import it (`internal/layers.Leaves`). It is not legacy, although its path is under `pkg/`.

**Field error:** One violation at one location. `validate.Error` is the domain shape (JSON Pointer, code, detail) that generated validators return in a `validate.Errors` list; `problem.FieldError` is the wire shape inside a problem's `errors` member. `http/problem` converts one to the other in one function.

**Issue:** One thing wrong with the configuration, a `config.Issue`: the setting path, its environment variable, where the value came from, the message and the redacted value. One bind reports every issue together in one `*config.Error`.

**Secret:** A sensitive value held as `secret.Secret` from the point it is read. Every printing path shows the redaction marker; `Reveal` is the one call that returns the value.

**Redacted:** The one marker, `[REDACTED]` (`secret.Redacted`), that every redacting path writes in place of a credential: a `Secret`, a credential-named log attribute, a URL password or query parameter, or a credential in a rejected config value.

**Adoption slice:** A bounded consumer migration to a framework capability, validated against the affected application.

Historical vocabulary decisions remain in the [decision log](docs/decision-log.md).
