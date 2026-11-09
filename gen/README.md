# gen

Nanostack code generators, and conventions for the third-party generators services run.

Generated application files remain visible and owned by each service.

Current packages:

- `gen` (this directory): the contract every generator follows, so the CLI, `gen/cmd/nano`, routes `nano gen <name>` to it. A generator has `Options` with a `Check` and an `Overlay` field, `Generate(Options) ([]gen.File, error)`, `Run(Options) error` (stale output wraps `gen.ErrStale`, written with `gen.Sync`) and a `Main(args, stdout, stderr) int` command. Every error is a `*gen.Error{File, Line, Msg, Fix}` with a fix, or several joined; `Main` prints them with `gen.Report`. Exit codes: 0 success, 1 source error or stale output, 2 bad usage, 3 environment failure (an error that wraps `gen.ErrEnvironment`). `Overlay` replaces files in every package load, keyed by absolute path. `db`, `api`, `config`, `errors`, `validate` and `wire` follow it; `db` and `api` have no `Overlay`, because they load no Go package, and `validate.Generate` returns a `*Result` that holds the `gen.File` and the schema features it does not enforce. `Report` prints `file:line: msg`, or `nano gen <name>: msg` when an error has no file, then `  fix: <fix>`; the library functions add no prefix.
- `pipeline`: `nano gen` with no generator name. It reads `nanogen/project.go` (types in [`core/nano/project`](../core/nano/project)), discovers the rest by parsing, and runs `db`, `api`, `validate`, `errors`, `config` and `wire` in that order, with `-check` overlays, orphan detection and the directive and `//go:generate` guards. Guide: [generation](../docs/generation.md#the-pipeline-nano-gen); decision: [ADR 0023](../docs/adr/0023-nano-gen-pipeline.md).
- `wire`: `nano gen wire`, which reads a `nano.Manifest` and writes `wire.gen.go` (`Build`, `BuildComponents`, `Overrides`, `Components`). It replaces Fx; the runtime it targets is [`core/nano`](../core/nano). Guide: [generation](../docs/generation.md#wiring-nano-gen-wire).
- `api`: `nano gen api`, which runs oapi-codegen v2.8.2 in-process and writes the server of the [HTTP boundary](../docs/http-boundary.md), its split by OpenAPI tag (`handlers.gen.go`, [ADR 0024](../docs/adr/0024-api-split-by-tag.md)) and the Go clients (see [generation](../docs/generation.md#the-api-step)). A binary that resolves another oapi-codegen exits 3.
- `validate`: `nano gen validate`, which writes request validators from the OpenAPI document (see [generation](../docs/generation.md#openapi))
- `db`: `nano gen db`, the pipeline's first step: it applies the migrations to an empty PostgreSQL database (a testcontainers container, or a scratch database on `NANO_GEN_DATABASE_URL`) and writes the go-jet models and table types, with `UniqueKeys` on each table type and `nanogen.sum`, which lets a run skip the database while nothing changed (see [generation](../docs/generation.md#database-nano-gen-db)). Without a database on a cache miss it exits 3.
- `config`: `nano gen config`, a reflection-free configuration binder, `CONFIG.md` and `.env.example` from a tagged Go struct (runtime: `core/config`)
- `errors`: `nano gen errors`, typed error constructors and `ERRORS.md` from a `//nano:errors` Go interface (runtime: `core/fault`)
- `internal`: helpers the generators share (import naming, package loading and its errors, and `gentest`, which fails a test on an error without a fix)

[docs/generation.md](../docs/generation.md) describes the pipeline, the tags, directives and `-check`.

## Module

This directory is the module `github.com/nanostack-dev/nanostack-framework/gen`. It requires the root module, and the root module never requires it, so `golang.org/x/tools`, oapi-codegen and the other generator dependencies stay out of the module graph of a service that only imports the runtime packages ([decision log](../docs/decision-log.md#2026-11-08-generators-move-to-a-nested-gen-module)). A service pins the CLI with a `tool` directive:

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>
go tool nano gen
```

Work on this module from this directory: `go test ./...`, `go -C gen test ./wire -update` from the repository root. Its lint configuration is [`.golangci.local.yml`](.golangci.local.yml); regenerate `.golangci.yml` with `go run ./lint/cmd/lintconfig -dir gen` from the root. [RELEASING.md](../RELEASING.md) tags it after the root module.
