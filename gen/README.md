# gen

Nanostack code generators, and conventions for the third-party generators services run.

Generated application files remain visible and owned by each service.

Current packages:

- `gen` (this directory): the contract every generator follows, so the CLI, `gen/cmd/nano`, routes `nano gen <name>` to it. A generator has `Options` with a `Check` field, `Generate(Options) ([]gen.File, error)`, `Run(Options) error` (stale output wraps `gen.ErrStale`, written with `gen.Sync`) and a `Main(args, stdout, stderr) int` command. Exit codes: 0 success, 1 source error or stale output, 2 bad usage. `config`, `errors`, `validate` and `wire` follow it; `validate.Generate` returns a `*Result` that holds the `gen.File` and the schema features it does not enforce. `Main` adds the `nano gen <name>: ` prefix to an error once; the library functions add none.
- `wire`: `nano gen wire`, which reads a `nano.Manifest` and writes `wire.gen.go` (`Build`, `BuildComponents`, `Overrides`, `Components`). It replaces Fx; the runtime it targets is [`core/nano`](../core/nano). Guide: [generation](../docs/generation.md#wiring-nano-gen-wire).
- `oapi`: oapi-codegen path and config conventions, including the server config of the [HTTP boundary](../docs/http-boundary.md)
- `validate`: `nano gen validate`, which writes request validators from the OpenAPI document (see [generation](../docs/generation.md#openapi))
- `jet`: go-jet database generation configuration conventions
- `config`: `nano gen config`, a reflection-free configuration binder, `CONFIG.md` and `.env.example` from a tagged Go struct (runtime: `core/config`)
- `errors`: `nano gen errors`, typed error constructors and `ERRORS.md` from a `//nano:errors` Go interface (runtime: `core/fault`)
- `internal`: helpers the generators share (import naming, package loading)

[docs/generation.md](../docs/generation.md) describes the tags, directives and `-check`.

## Module

This directory is the module `github.com/nanostack-dev/nanostack-framework/gen`. It requires the root module, and the root module never requires it, so `golang.org/x/tools` and the other generator dependencies stay out of the module graph of a service that only imports the runtime packages ([decision log](../docs/decision-log.md#2026-11-08-generators-move-to-a-nested-gen-module)). A service pins the CLI with a `tool` directive:

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>
go tool nano gen wire -dir cmd/app
```

Work on this module from this directory: `go test ./...`, `go -C gen test ./wire -update` from the repository root. Its lint configuration is [`.golangci.local.yml`](.golangci.local.yml); regenerate `.golangci.yml` with `go run ./lint/cmd/lintconfig -dir gen` from the root. [RELEASING.md](../RELEASING.md) tags it after the root module.
