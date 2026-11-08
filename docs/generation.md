# Generation

Generation remains explicit. The framework standardizes inputs, paths, and validation; it does not hide `oapi-codegen`, migrations, or go-jet.

## OpenAPI

Convention:

- source spec: `cmd/http/openapi.yaml`
- server output: `internal/apigen/gen.go` or the app's chosen generated package
- generated clients stay in app-owned client folders
- cleaned client specs are generated from schema-aware tooling, not ad hoc regex-only transforms

Framework home:

- `gen/oapi` contains config conventions and future helpers
- `gen/validate` is `nano gen validate`, which writes the request validators
- the [pipeline](#the-pipeline-nano-gen) runs `validate` on the spec that `nanogen/project.go` declares; its api step will run `oapi-codegen` in-process

### Server generation

The server is oapi-codegen's strict server on `net/http`'s ServeMux, with its strict template replaced by the override in `http/nanohttp/oapitemplates`. The override decodes bodies through `nanohttp.Decode`, runs the generated validators and sends every error through `http/problem`. [The HTTP boundary](http-boundary.md) describes the runtime contract.

`oapi.Config.ServerCodegenYAML` returns this config with the template inline. Written by hand, it is:

```yaml
package: apigen
output: gen.go
generate:
  models: true
  std-http-server: true
  strict-server: true
additional-imports:
  - package: github.com/nanostack-dev/nanostack-framework/http/nanohttp
  - package: github.com/nanostack-dev/nanostack-framework/http/problem
output-options:
  user-templates:
    strict/strict-http.tmpl: <path to http/nanohttp/oapitemplates/strict/strict-http.tmpl>
    union.tmpl: <path to http/nanohttp/oapitemplates/union.tmpl>
```

`union.tmpl` makes the `As<Variant>` and `Discriminator` methods of a `oneOf` or `anyOf` type decode with `nanohttp.Unmarshal`, the strict json/v2 options the validators use. The upstream template decodes with `encoding/json` v1, which matches member names case-insensitively: a handler could read `"TOKEN"` while validation checked `"token"`.

The overrides are pinned to oapi-codegen v2.8.2 (`oapitemplates.OapiCodegenVersion`). To upgrade oapi-codegen, diff the upstream `strict/strict-http.tmpl` and `union.tmpl` against the overrides.

### Generate order

Run oapi-codegen first, then the pipeline. `nanogen/project.go` declares the spec and the server package, and `nano gen` runs `validate` on them before the other steps ([the pipeline](#the-pipeline-nano-gen)):

```go
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml openapi.yaml
```

```sh
go generate ./internal/api && go tool nano gen
```

The override calls the `Validate` and `validateParams` methods that `nano gen validate` writes, so the package does not compile until they exist and match the spec. CI runs `nano gen -check` to fail on a stale `validate.gen.go`. Until the pipeline's api step lands (design PR 4), the oapi-codegen line stays in `go generate`.

### Unenforced schema features

A schema feature the validators cannot enforce, such as a `oneOf` over a Go type that is not an oapi-codegen union, fails `nano gen validate` with exit status 1, at the line of the schema. The generated file is still written, and it lists every gap. Change the schema, or accept the gap with the `x-nano-unenforced` extension. Its value is the reason, which may not be empty. It covers that schema and every schema under it:

```yaml
Shape:
  x-nano-unenforced: "the handler checks the variant"
  oneOf: [...]
```

An extension that accepts no gap is an error too, so it cannot hide the next one. The extension replaces the `-allow` flag ([ADR 0023](adr/0023-nano-gen-pipeline.md)).

### Patterns

`pattern` is an ECMA-262 regular expression. `nano gen validate` translates it to RE2 at generation time and fails on what RE2 cannot express (lookaround, backreferences). Where ECMA-262's u and non-u modes differ, the u mode is the reference: a pattern matches code points, and `\u{...}`, `\p{...}` and surrogate pairs such as `\uD83D\uDE00` are one character. An escape of a letter or digit that ECMA-262 does not define, such as `\a`, `\z`, `\Q` or `\pL`, is an error, because RE2 gives it another meaning. A backslash before punctuation stands for the character itself.

## Go-Jet

Convention:

- migrations are applied before generation
- generated output goes under `internal/db/gen`
- custom type mappings are explicit, including `text[] -> pq.StringArray`
- database/env names are parameterized by service

Framework home:

- `gen/jet` contains generation configuration conventions
- `cli generate db` should replace duplicated `dbgen.go` and brittle shell generation flows over time

## Configuration: `nano gen config`

Package: `gen/config`. Runtime: `core/config`.

Source: a plain Go struct. Struct tags declare each setting, and the field's doc comment is its description.

- `config:"name[,optional]"` names the setting in lower snake case. Nested structs add a path segment (`billing.due_days`). Every exported field needs one; `config:"-"` skips a field.
- `env:"NAME"` overrides the environment variable, derived from the path by default (`BILLING_DUE_DAYS`).
- `default:"value"` is parsed and validated at generation time. Defaults of `encoding.TextUnmarshaler` types (such as `slog.Level`) are checked only when `Bind` runs; bind the defaults in a test.
- `validate:"min=1,max=365"`, `oneof=a b c` and `scheme=https` are the rules.
- A setting without a default is required unless it is optional. Secrets (`secret.Secret` from `core/secret`, the framework's one secret type) cannot have a default and are never read from YAML files. Every printing path shows them as `[REDACTED]`.

Outputs, next to the struct: `config.gen.go` (a reflection-free `Bind`, a redacted `LogValue`, `String` and `GoString`), `CONFIG.md` (the reference of every setting) and `.env.example` (`-env` sets another path).

`nano gen config` loads the package with `-tags=nanogen`, as `nano gen wire` does. That drops `wire.gen.go` and the files that call `Build` (they carry `//go:build !nanogen`), so a renamed `Config` field never blocks regeneration: config writes the new `config.gen.go`, then wire reads it. Keep `Config` in a file without a build tag. The generator replaces its own previous output with a stub, so a stale `config.gen.go` does not block it either.

At start-up, `config.NewSources(config.ProfileFromEnv("config"), config.Env())` layers `config/base.yaml`, then `config/<NANO_PROFILE>.yaml`, then environment variables. `Bind` reports every issue in one `*config.Error` (a list of `config.Issue`) and never shows a secret or a URL password.

## Errors: `nano gen errors`

Package: `gen/errors`. Runtime: `core/fault`. Decision: [ADR 0022](adr/0022-error-catalogue-from-go-interface.md).

Source: one Go interface per package whose doc comment holds `//nano:errors namespace=<name> [type_base=<url>]`. Each method declares one error; its doc comment holds `//nano:error code=<snake> status=<4xx|5xx> title="..." [detail="... {param} ..."]`. `code=` is required: the code and the problem type URI are the client contract and never follow a rename of the Go method.

Outputs, next to the declaration: `errors.gen.go` (a constructor, a typed error and an `Err<Name>` sentinel per error, plus the `Kinds` list) and `ERRORS.md` (the catalogue). The generator type-checks the package and its own output, so a declaration either generates code that compiles or fails with its `file:line`.

## The pipeline: `nano gen`

`nano gen` with no generator name runs every generation step of a module, in a fixed order: `validate`, `errors`, `config`, `wire`. [ADR 0023](adr/0023-nano-gen-pipeline.md) records the design; package [`gen/pipeline`](../gen/pipeline) implements it.

```sh
go tool nano gen                 # run every step, write what changed
go tool nano gen -check          # write nothing; exit 1 listing every stale, missing or orphaned file
go tool nano gen -check -diff    # also print the first 20 lines of each diff
go tool nano gen -only config    # these steps, still in pipeline order (-skip is the inverse)
go tool nano gen -list           # print the plan: step, package, inputs, outputs
go tool nano gen -json           # one JSON event per line
go tool nano gen -dir apps/x     # the directory to run on; default: the module root
```

**The project declaration.** `nanogen/project.go` holds the inputs that nano cannot discover. It has no build tag, so `go build`, `go vet` and gopls type-check it, and nothing imports it. nano reads its composite literal from source, so every value must be a literal or a constant. Paths are relative to the directory nano runs on.

```go
// Package nanogen declares the inputs of this module's code generation that
// cannot be discovered. Run: go tool nano gen
package nanogen

import "github.com/nanostack-dev/nanostack-framework/core/nano/project"

var Project = project.Project{
	API: &project.OpenAPI{
		Spec:   "cmd/http/openapi.yaml",
		Server: project.Package{Dir: "internal/api"},
	},
	Env: "../../.env.example", // default: .env.example next to Config
}
```

A module without the file still runs the steps nano discovers. `nano gen -list` then prints a declaration filled with the OpenAPI document and the server package it finds.

**Discovery.** nano parses the tree and never type-checks it, so discovery works on a tree that does not compile. It skips `testdata`, `vendor`, `node_modules`, hidden directories, directories that start with `_`, and nested modules, which run their own `nano gen`.

| Step | Runs on | Writes |
| --- | --- | --- |
| validate | the `API` of the declaration | `validate.gen.go` in the server package |
| errors | each package with a `//nano:errors` declaration | `errors.gen.go`, `ERRORS.md` |
| config | each package with a `//go:build nanogen` file that calls `nano.Manifest` | `config.gen.go`, `CONFIG.md`, the `Env` file |
| wire | the same packages | `wire.gen.go`, in the manifest package |

**Order and failures.** Each step loads the fresh output of the steps before it. Without `-check`, a step writes before the next one loads. With `-check`, nothing is written: later loads see the fresh outputs as a `go/packages` overlay. In both modes, an existing output of a later step is replaced in each load by a stub whose function bodies panic, so a stale later output cannot break an earlier step. A failed step skips the steps that need it (config needs validate and errors; wire needs config), and the other steps still run. validate and config read `nanogen/project.go`, so a declaration with an error skips them, and wire. A validation gap fails the run but skips nothing.

**Orphans.** Every output starts with `Code generated by nano gen <step>`. A file with that header, for a step that ran, that the run did not produce, is an orphan: `nano gen` deletes it and `-check` fails on it. So a deleted catalogue cannot leave its generated code behind, and neither can an `API` removed from the declaration. A skipped step has not run, so its files stay.

**Guards.** An unknown `//nano:` directive is an error at its line. So is a `//go:generate` line that runs `nano gen`: the pipeline runs that step in its order. Delete the line and run `go tool nano gen`.

**Output.** A clean run prints nothing. Otherwise it prints one line per file (`wrote`, `deleted`, `stale`, `missing`, `orphan`), then each problem:

```text
stale cmd/app/config.gen.go
stale cmd/app/wire.gen.go
  fix: run nano gen without -check, then commit the result
greeterr/doc.go:3: unknown directive //nano:mapers
  fix: known directives are //nano:errors and //nano:error
nano gen wire: skipped: needs config
```

With `-json`, each event is one line on stdout: `{"step", "level", "file", "line", "msg", "fix"}`, plus `"diff"` with `-diff` and `"inputs"` and `"outputs"` with `-list`. `level` is one of `wrote`, `deleted`, `stale`, `missing`, `orphan`, `skipped`, `error` and `plan`. A problem found before any step runs, in the declaration, in discovery or by a guard, has the step `plan`.

## Running one generator

`nano gen <name>` runs one generator: `nano gen config`, `nano gen errors`, `nano gen validate` or `nano gen wire`. Use it for a single run and in tests; the pipeline runs every step. `nano gen <name> -h` lists the flags of one generator.

`nano` and the generators are in the nested module `github.com/nanostack-dev/nanostack-framework/gen`, so their dependencies (`golang.org/x/tools` among them) stay out of the module graph of a service that only imports the runtime packages. A service pins `nano` with a `tool` directive and runs it with `go tool`:

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>
go tool nano gen
```

The `gen` module requires the root module, so the framework's own root module cannot pin `nano` as a tool. It runs `go -C <path to gen> run ./cmd/nano gen -dir <directory>` instead: CI runs it with `-check` on [`internal/nanoexample`](../internal/nanoexample/doc.go). [RELEASING.md](../RELEASING.md#consumers-of-nano) gives the tag order.

Every generator, and the pipeline, takes `-check`: it writes nothing and exits 1 when a committed output is missing or differs from what the source produces. Run `nano gen -check` in CI next to the tests. Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | Success. `-h` prints the flags and exits 0. |
| 1 | A source error, stale output, an orphan, or (for `validate`) a validation gap that no `x-nano-unenforced` accepts. |
| 2 | Bad usage. |
| 3 | The environment failed: the go command cannot load packages, or a file cannot be read or written. |

Every error names its fix. A generator prints each error as its position and message, then an indented `fix:` line:

```text
config.go:15: Name: the field has no config tag
  fix: add a config:"name" tag, or config:"-" to skip the field
nano gen config: generated files are stale: config.gen.go
  fix: run nano gen config
```

An error with no file starts with `nano gen <name>:` instead of a position.

## Wiring: `nano gen wire`

`nano gen wire` replaces Fx. It reads a manifest, resolves the provider graph at generation time and writes plain Go: `wire.gen.go` with `Build` and `BuildComponents`. A missing provider, a cycle or a bad signature is a generation error with `file:line` and a fix, not a panic at boot. The runtime is [`core/nano`](../core/nano): the `App`, the lifecycle interfaces and `Main`. It uses no reflection.

### The manifest

A manifest is a package-level variable in a file with the `nanogen` build tag, so it never links into the service:

```go
//go:build nanogen

package billing

var App = nano.Manifest("billing",
	nanopg.Module,                      // a module variable from a library
	nano.Providers(invoice.NewService, invoice.NewPGStore),
	nano.Bind[invoice.Store, *invoice.PGStore](),
	nano.Collect[http.Handler](invoice.NewHandler, admin.NewHandler),
	nano.Invoke(RegisterMetrics),
)
```

| Entry | Meaning |
| --- | --- |
| `nano.Providers(fns...)` | Package-level constructors. Each provides its result type. |
| `nano.Module("name", items...)` | A named group of entries in ordinary code, so a library ships them as one value. |
| `nano.Bind[I, T]()` | Satisfies interface `I` with the provided `T`. Bindings are explicit: providing `*PGStore` does not satisfy `Store` by itself. |
| `nano.Collect[T](fns...)` | Provides `[]T`, one element per listed function, in declaration order across the manifest and its modules. Members do not provide their own type, so several may return the same type. `Collect[T]()` with no function declares an empty collection. |
| `nano.Invoke(fn)` | Calls `fn` after construction, in declaration order. `fn` returns nothing or `error`. |

Build also provides `context.Context` (Build's `ctx`), `*nano.App`, the manifest package's `Config` struct and each of its exported fields. A `nano.AppConfig` field of `Config` configures the App's timeouts.

Signature rules:

- A provider or collection member is `func(deps...) T`, `func(deps...) (T, error)` or `func(deps...) (T, func(), error)`. The `func()` is a cleanup that runs at stop, also when a later provider fails.
- Two providers of one type are an error. Two `Config` fields of one type are ambiguous to a consumer that takes that type.
- A provider whose type has a lifecycle method on the pointer receiver must return the pointer.
- In the manifest's own package, an unused provider, binding or collection is an error. In a library module it is skipped. There is no warning mode: the generator either writes code or fails.

### Lifecycle

A component opts in by implementing an interface. The generator sees it on the provider's result type and emits `app.Manage(name, v)` with a comment that names what it found:

| Interface | When the App calls it |
| --- | --- |
| `Starter` `Start(ctx) error` | Once, in dependency order, under `StartTimeout`. Work that can outlast it, such as database migrations, belongs in a `nano.Invoke` (see `pg/nanomigrate`). |
| `Stopper` `Stop(ctx) error` | Once, in reverse order, only if its `Start` succeeded (or it has none). |
| `Runner` `Run(ctx) error` | After every Starter, until `Stop` cancels `ctx`. Returning earlier shuts the App down and is an error. |
| `HealthChecker` `CheckHealth(ctx) error` | For `/readyz`. The name is specific so that an unrelated `Check` method is not a probe. |

`Manage` registers a component once. A provider that returns one of its dependencies as an interface (`func AsHandler(s *Server) Handler { return s }`) does not run or stop the server twice: the same pointer, or an equal comparable value, is skipped. A value whose static type is an interface gets its lifecycle from its dynamic type at run time.

Stop cancels the Runners, waits for them, then calls the Stoppers one at a time in reverse order. The drain and the Stoppers share `StopTimeout`. A Stopper called after that deadline still gets `nano.StopGrace` (one second), so a slow Runner or Stopper never cuts off a later flush. Stop returns within `StopTimeout` plus at most `StopGrace` per Stopper called after it. A Runner or Stopper that overruns is reported and left running.

`nano.Main` exits `0` after SIGINT or SIGTERM, also when the signal arrives during build (a migration or a ping returns the cancellation) or during start (`ErrShutdownDuringStart`) and every stop succeeds. It exits `1` on a build, start, Runner or stop failure, including a failure next to the cancellation. `AppConfig.DrainDelay` keeps the Runners serving for that long after readiness turns unavailable, so a load balancer stops routing to the instance before its listeners close; it is counted inside `StopTimeout`, and Start fails when it leaves the Runners less than `StopGrace` of it.

### Overrides and Components

Tests replace any provided value through the generated, typed `Overrides`. A nil field keeps the production provider. A set field skips that provider and every provider only it needed, and the App manages the override's lifecycle instead. Starters and Runners follow the same rule: a Starter or Runner that something takes is built only when a consumer that is built needs it, so overriding `Store` skips the real `*PGStore` and a pool that only it used, even when they ping on `Start`. A Starter or Runner that nothing takes is always built. A collection member is built only with its collection, so a Runner listed only in a `Collect` that nothing takes does not run (an error in the manifest's package, skipped in a library). Without overrides every value in the graph is built.

Override a collection as a whole: set the `[]T` field. A non-nil empty slice overrides it with no elements.

`BuildComponents(ctx, cfg, ov)` returns the App and a `Components` struct with a field per value (named like the `Overrides` fields; a collection member is named after its function). A field is named after its type (`*billing.Service` gives `Service`). When two values share a type name, both are qualified by their package (`*nanopg.DB` and `*sql.DB` give `NanopgDB` and `SQLDB`), so adding or reordering a provider never renames another field. A slice of a name that ends in `s` gets `List` (`[]log.ContextAttrs` gives `ContextAttrsList`). A field holds the override when one was set, and the zero value when no built value needed it. This replaces `fx.Populate` in integration suites.

### Migrating from Fx

| Fx | Nanostack |
| --- | --- |
| `fx.Provide`, `fx.Module` | `nano.Providers`, `nano.Module` |
| `fx.Supply(v)` | A provider that returns `v`, or a `Config` field |
| `fx.Lifecycle` hooks | Implement `Starter`, `Stopper`, `Runner`; return a cleanup `func()` |
| `fx.Annotate(..., fx.As(new(I)))` | `nano.Bind[I, T]()` |
| Value groups, `fx.ResultTags(group)` | `nano.Collect[T](fns...)` in each contributing module; consumers take `[]T` |
| `fx.Invoke` | `nano.Invoke` |
| `fx.Populate(&targets...)` | `BuildComponents` and the `Components` fields |
| `fx.Replace`, and `fx.Decorate` in tests | A field of `Overrides` |
| `fx.Decorate` in production that returns the same value (for example it starts a listener on a client) | `nano.Invoke` with a function that takes the value |
| `fx.Decorate` in production that wraps the value | A provider of a distinct wrapper type, bound with `nano.Bind` where consumers take an interface |
| `fx.ValidateApp` | `nano gen -check` in CI |

### Commands

```text
nano gen wire [-dir .] [-out internal/gen/wire] [-tags a,b] [-check]
```

`-out` is relative to `-dir`; set it to `.` to generate into the manifest package. The pipeline always writes `wire.gen.go` into the manifest package. `-check` writes nothing and exits 1 when `wire.gen.go` is missing or stale. Output is deterministic. Generated provider errors read `nano: provide *T: pkg.NewT: <cause>`, without a source position, so moving a provider does not change the generated file.

Every generator follows the contract of package [`gen`](../gen/gen.go): `Options` with a `Check` field and an `Overlay` field, `Generate(Options) ([]gen.File, error)`, `Run(Options) error` (stale output wraps `gen.ErrStale`) and `Main(args, stdout, stderr) int`. `gen/validate` is the one variation: its `Generate` returns a `*Result` that holds the `gen.File` and the schema features it does not enforce.

- Every error is a `*gen.Error{File, Line, Msg, Fix}`, or several joined with `errors.Join`. A `Fix` is required: an error without one is a generator bug, and each generator's error table test fails on it. `gen.Errorf` takes the fix before the message.
- An error of the machine wraps `gen.ErrEnvironment`, and `Main` exits 3 on it.
- `Overlay` replaces files in every `go/packages` load, keyed by absolute path. The overlay never replaces a generator's own output: `config` loads a stub of it, `errors` and `validate` an empty file, and `wire` loads with `-tags=nanogen`, which drops `wire.gen.go`. A caller that keeps outputs in memory, such as a `-check` run of several generators, overlays the fresh outputs of the earlier ones.
- `Main` prints with `gen.Report`. `Generate`, `Run` and `gen.Sync` add no `nano gen <name>:` prefix, and a Check run returns a `*gen.StaleError` that lists the stale files.

`gen/cmd/nano` routes `nano gen <name>` to each `Main`.

## Replacing Shell Scripts

Existing app scripts such as `generate_anchor.sh` and `generate_echopoint.sh` should be treated as migration sources. The framework path is:

1. Keep current scripts while framework generation is introduced.
2. Move shared generation configuration into `gen/oapi` and `gen/jet`.
3. Add CLI commands that call visible tools and config files.
4. Switch app scripts to thin wrappers or remove them after `anchor` and `echopoint` validation passes.
