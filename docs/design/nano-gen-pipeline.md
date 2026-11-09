# `nano gen` pipeline, generated mappers and the API split by tag: design

- Status: proposed
- Date: 2026-11-08
- Branch: `feat/nano-gen-pipeline`, off `main` `ac9c78b` (`v0.23.0-beta.1`)
- Inputs: four read-only studies (generation inventory, mapper survey, tag survey, prior art), three competing designs and the judge verdict. The judge picked the agent-first pipeline, the agent-first mappers and the convention tag split, each with grafts from the others. This document is that result, with every factual error the judge found corrected and every file claim checked again against the source.

This document decides how one command, `nano gen`, runs every code generator of a service, how api, domain and entity structs are mapped by generated code instead of by hand, and how the single strict server interface is split by OpenAPI tag. Each decision is judged on end-state quality ("would we pick this from scratch?"), not on the size of the change.

Path prefixes: `F` is this worktree. `A` is `anchor-worktrees/framework-beta/apps/anchor` and `A-root` its repository root. `E` is `echopoint-worktrees/framework-beta`. All three are under `/Users/alexisgardin/Documents/NanostackProject/`. Nothing in `A` or `E` was changed; every number about them comes from read-only scans.

Label note: the designs are called A (convention), B (explicit) and C (agent-first). Design A titles itself "Design C" in its own text. This document uses A, B and C as above.

## Problem

**Generation is a shell script per service, and half of it is never checked.**

| Fact | Evidence |
|---|---|
| A service has 12 generation steps across docker, the `migrate` CLI, openssl, sed, `go generate`, `go run`, `nano gen` and pnpm. | Generation study, steps table |
| `nano gen` runs one generator per call. The roadmap wants a no-name run in a fixed order but has no input file for it. | `F/cmd/nano/main.go:25-30,45-55`, `F/docs/roadmap.md:26` |
| CI never checks the oapi server (`A/internal/api/gen.go`, 26k lines; `E/internal/apigen/gen.go`, 44.6k lines), the Go clients or the DB models. | `A-root/.dagger/modules/anchor/main.dang:145`, `E/.dagger/modules/echopoint/main.dang:139` |
| Both services resolve oapi-codegen v2.8.0. The nano templates are pinned to v2.8.2. | `A/go.mod:24`, `E/go.mod:32`, `F/http/nanohttp/oapitemplates/oapitemplates.go:44` |
| config and wire can block each other in `cmd/app`. `wire.gen.go` reads Config fields; config loads without tags and refuses any foreign compile error; wire then loads a stale `config.gen.go`. | `F/gen/config/load.go:66-89`, `F/gen/wire/load.go:105-126`, `A/cmd/app/wire.gen.go:650` |
| The DB generator deletes its schema directory and writes straight to disk, so it has no `-check`. | `F/db/generator/template/process.go:32` |
| Anchor's Go client is generated from a gitignored file that sed produces. | `A-root/scripts/client-spec.sh:15-17`, `A-root/.gitignore:107` |

**Mapping code is large, mostly mechanical, untested, and already buggy.**

| | anchor | echopoint |
|---|---|---|
| Mapper functions / lines | 147 / 2,345 | 213 / 3,965 (plus 14 client mappers) |
| Inline mapping literals / lines | 182 / 923, of which 141 build api→domain inputs in 113 handlers | 90 / 399 |
| Fields that are exact or initialism-only copies | 68% | 63% |
| Fields a MapStruct-style generator covers | about 86% | about 84% |
| Fields that need initialism-aware matching (`Id`/`ID`, `Url`) | 18% | 11% |
| api→domain functions that are strictly mechanical | 0 of 29 (each needs `TenantID`) | 11 of 53 |
| Mapper tests | 1 of 97 names called from a test | 15 of 207 names |

Across both repos there are 38 `*Mapper` structs; 35 have no fields and exist only to be wired.

Four bugs are confirmed in the source:

1. Anchor `A/internal/mapper/user_mapper.go:13,26` never maps `Name` (`model.Users.Name` is `*string`, `auth.User.Name` is `string`) and `ToEntity` never sets `model.Users.Status`. `users.status` is `NOT NULL` with no default (`A/migrations/000001_init.up.sql:30`), so every insert writes `''`. `auth.User` has no `Status` field (`A/internal/domain/auth/user.go:9-17`).
2. Anchor `ProductAPIKeyResponse.ExpiresAt` is in the contract, but no domain or model type has it, so it is never set.
3. Echopoint `MapDomainExecutionToResponse` (`E/internal/feature/executions/api_mapper.go:18`) never sets `ScheduleRunId` or `TimeoutSeconds`.
4. Three policies for a stored JSON value that fails to decode: log and drop, ignore, return the error.

**One strict server interface carries every operation.** Anchor has 133 operations in 21 tags, served by one `AnchorAPI` whose `NewAPI` takes 31 parameters, two of them unused (`TenantService`, `Queue`). Echopoint has 189 operations in 20 tags, served by 22 embedded feature handlers (the "split by inheritance"). Embedding gives silent shadowing, ambiguous selectors and promoted helpers. Anchor's tags already match its files. Echopoint's do not: 8 tags span several handlers.

## Decisions

| # | Decision | Choice | Rejected alternatives, and why |
|---|---|---|---|
| P1 | Entry point | `go tool nano gen` with no name runs every step in a fixed order. `-check` writes nothing and lists every stale, missing or orphaned file. `nano gen <name> -dir …` stays for single runs and tests. | **`go generate`**: no order, no check, and it would run echopoint's network-bound `integrationgen`; the oapi lines hide behind `//go:build tools`. **Shell scripts**: not portable, not checked. |
| P2 | Where inputs come from | A typed Go declaration, `nanogen/project.go`, holds only what cannot be discovered: the spec, the server and client packages, the DB image, migrations, output root and type overrides, the `.env.example` path. `nano` reads it **statically** (literal values only), the way `gen/wire` reads `nano.Manifest`. Everything else is discovered by parsing. | **`nano.yaml`** (buf, sqlc): no compiler, no rename support, a second schema. **Markers only** (A): the spec path in a comment is untyped, the DB image and output root still need a home, and the roadmap asks for a per-project file. **`go run ./nanogen`** (C): a second binary and a cold compile. **Declared membership lists** (B): a mapper package must be listed twice, so an agent makes two edits. |
| P3 | Order | Fixed by the framework, steps run one after another: db, api, validate, errors, repo, config, wire; mapper (part 2) joins before repo and lint after wire. repo needs db; config needs repo. | **Parallel steps** (buf): wire type-checks the output of every earlier step. **Order in the project file**: a project cannot know the loaders' constraints. |
| P4 | config/wire deadlock | config loads with `-tags=nanogen`, as wire does. That drops `wire.gen.go` and `run.go`/`app.go(E)` (all `//go:build !nanogen`) and keeps `manifest.go`, which wire already proves compiles that way. | **Stubs that hold only a package clause** (B): `run.go:61` and echopoint `app.go:22` call `Build(ctx, cfg, Overrides{})`, declared in `wire.gen.go:75,411`, so config fails on every run. **Deleting outputs to recover**: loses the tracked files. |
| P5 | Generate and check modes | Generate writes after each step, so later steps load real files. `-check` keeps outputs in memory and gives later loads a `go/packages` overlay. An output whose directory does not exist is reported `missing`, never overlaid. Earlier loads see a stub of each later output: the body-stripped stub of the file on disk, or a forward stub that the step supplies for its outputs. The repo step's forward stub declares, by parsing alone, a panicking method for each bound interface method without a hand-written one, so an interface that just gained a method breaks no earlier step (landed with PR 8). | **Everything in memory until the end** (C): overlays cannot add whole new directories, such as a fresh DB tree. **A temp copy of the module**: the overlay does the same job. |
| P6 | Orphans | Every output starts with `Code generated by nano gen <step>. DO NOT EDIT.` A file under a declared or discovered output directory with that header, for a step that ran, that the run did not produce, is an orphan: generate deletes it, `-check` fails on it. Only inside the module. | **`outputs.txt` manifest** (B): churns on every change. **A lock file**: restates what the header says. |
| P7 | Errors and exit codes | Every error is `gen.Error{File, Line, Msg, Fix}`; an error without a fix is a framework bug, and a test enforces it. `-json` writes one JSON event per line. Exit 0 OK, 1 source error or stale or orphan, 2 usage, **3 environment** (Docker, database, toolchain). A failed step skips the steps that depend on it; independent steps still run. A validate gap does not stop later steps. | **Stop at the first failure** (`go generate`): an agent needs every fix from one run. **Text-only output** (A): agents parse it. |
| P8 | oapi-codegen | Linked in-process as a library in the `gen` module, pinned at v2.8.2 with the `oapitemplates`. A build-info check fails on any other resolved version, with the `go get` fix. kin-openapi v0.149.0 builds with it (judge's scratch build). | **Subprocess through a `tool` directive**: two pins to keep in step. **`go run pkg` without a version**: today's v2.8.0/v2.8.2 mismatch. |
| P9 | DB step | testcontainers plus the framework migrator in-process. An explicit output root. `nanogen.sum` lets a run skip the database when inputs and outputs match; CI always passes `-fresh`. `NANO_GEN_DATABASE_URL` uses an existing server. The step also writes `UniqueKeys()` on each table type, the keys that match at most one row, and `ColumnTypes()` and `ForeignKeys()` (search design G1), which `nano gen repo` reads without a database. The runner owns the gen database for the run (started on first use, stopped at the end); only the db step opens it. | **No cache** (B): an agent without Docker cannot run the pipeline after a handler change. **A sum that lets `-check` pass without Docker in CI** (A): CI would never prove the models regenerate the same. |
| P10 | Other modules | A separate module (anchor `A-root/clients/go`, its own `go.mod`) has its own `nanogen/project.go` and its own run. Inputs may sit outside the module; outputs may not. | **Writing and orphan-scanning across the module boundary** (C). |
| P11 | Validate waivers | `-allow` becomes an `x-nano-unenforced: "<reason>"` extension on the schema it waives. | **`ValidateAllow` in the project file** (C): far from the schema. **No place at all** (B). |
| P12 | Guards | An unknown `//nano:` directive is an error at file:line. A `//go:generate` line that runs `nano` or oapi-codegen is an error. No arbitrary `Exec` steps. `-list` prints the discovered plan. | **Silent ignore**: a misspelled marker would drop a step. **Exec steps**: they break `-check`. |
| M1 | Mapper tool | Build `nano gen mapper` in-house. | **goverter**: `extend` cannot be generic (issue #235), so every `Option[T]` needs a wrapper; unused source fields pass; untyped comment DSL (#196); no checked inverse; four Go-version breaks including 1.27.2. **convgen as a dependency**: v0.1.4, no users. **copier and other reflection mappers**: drift becomes a silent zero (ADR 0013, ADR 0015). |
| M2 | Declaration form | Typed Go: `nanomap.Func[S, D]("Name", rules...)` inside `func _() { … }`, in a file **with no build tag**. Field references are Go expressions (`auth.User{}.Name`), resolved through `types.Info`. `go build`, `go vet` and gopls type-check them, and a gopls rename updates them. | **`//nano:map` comments on an interface** (A, B): untyped, a rename does not update them. **`//go:build nanogen` declaration files** (C): default `go vet` and gopls skip them, so "a typo is a compile error" holds only under the tag. |
| M3 | Name | The command and package are `mapper` (`nano gen mapper`, `mapper.gen.go`). ADR 0015's internal row-scan mappers in `gen/db` are renamed **scanners**. | **`convert`** (C): renames the user's word to avoid a clash that a one-line ADR edit removes. |
| M4 | Policy | Unmapped fields are errors on both sides; waivers name one field and need a reason. `*T`→`T` and `Option[T]`→`T` are errors unless declared. Reverse is strict. string→enum is fallible by default. JSON decode has one policy: return the error. | **Warning modes** (MapStruct defaults): the decision log rejects them. **Automatic `NilAsZero` reversal** (C): `''` would come back as NULL silently. **`OrElse(zero)` for `Option[T]`→`T`** (A): the same silent loss A forbids for pointers. |
| M5 | Output and placement | Plain functions next to the declaration. api↔domain declarations live in `internal/api`, domain↔entity in `internal/mapper` (anchor) or the feature package (echopoint). The 38 `*Mapper` structs and their providers are deleted. | **Generated struct implementations** (MapStruct `XImpl`): pure functions need no DI. **All mappers in `internal/mapper`**: api mappers need `api` types and `api` calls them, an import cycle. |
| M6 | Signatures | A rule that can fail makes the function return `(D, error)`. A `Via` function that takes `context.Context` first makes the function take `ctx` first. Extra inputs are explicit `Arg` parameters. | **Mappers that need a context are hand-written** (B): collections-style mappers would stay manual. **Parameters matched by name** (A): implicit. |
| M7 | Overrides | Three levels. **One field:** `From(target, source)` for a source field with another name, `Via` for a function you write, plus `Default`, `Arg`, `JSON`, `Cast` and `Flatten`. **After the copy:** `After` with `Computed`. **One type pair:** `Use(fn)` registers a hand-written function for every nested or element use of that pair, or the whole mapper is hand-written and the `Func` is left out. A declared `Func` and a hand-written function of the same name, or a `Func` and a `Use` for the same pair, is an error. | **Renames through an identity `Via`**: it hides a rename as a conversion, and `Reverse` cannot invert it. **Source paths as strings** (MapStruct `@Mapping(source = "a.b")`): untyped, a rename does not update them. **Repeating `Via` on every field of a hand-mapped nested type**: one decision written N times. **Finding hand-written functions by signature**: implicit, and two candidates would need a rule to pick one. |
| T1 | Split mechanism | The `api` step writes `handlers.gen.go` in the oapi package: one `<Tag>Server` interface per tag, a `Handlers` struct, one delegator per operation, and `var _ StrictServerInterface = (*Handlers)(nil)`. | **One package per tag**: shared schemas become distinct types (anchor 396 types instead of 253, echopoint 588 instead of 403). **Forking `strict-interface.tmpl`**: a third upstream template to compare on each upgrade. **Embedding handlers** (echopoint today): silent shadowing. **Embedding interfaces**: a nil field panics at request time. **`x-nano-handler`**: a second grouping axis. |
| T2 | Handler wiring | A handler constructor returns its tag interface: `func NewEmailHandler(…) EmailServer`. A missing operation is a compile error in the handler's own file. No `nano.Bind` lines. Cost: the constructor returns an interface, against Go's "return structs" habit; its only consumer is `NewHandlers`, so a concrete return buys nothing. | **One `nano.Bind` per tag** (B, C): 21 extra lines in anchor, and since `Bind[I, T any]` has no constraint (`F/core/nano/manifest.go:65`) the error is a wire error at the Bind line, not a compile error. |
| T3 | Spec rules | Every operation has exactly one tag. Every used tag is declared and every declared tag is used. Two tags with the same Go name are an error. Errors point at `openapi.yaml:line`. | **A default bucket for untagged operations**: hides a missing decision. **Several tags per operation**: a tag is now a code boundary. |
| T4 | Method names | From `codegen.OperationDefinitions`, called after the server `codegen.Generate` and before any client run. | **Parsing `// (METHOD /path)` comments** (B, tag study): fragile, and the step already has the operation list. |
| T5 | `Handlers` fields | Unexported, set once by `NewHandlers`, which refuses a nil handler and names its tag. | **Exported fields** (A, C): a caller can set one to nil after the check. |

## Feature 1: the `nano gen` pipeline

### Design

**Command surface.**

```
go tool nano gen                     # run every step in order, write what changed
go tool nano gen -check              # write nothing; exit 1 listing every stale, missing or orphaned file
go tool nano gen -check -fresh       # CI: also ignore the DB cache
go tool nano gen -only api,mapper    # these steps, still in pipeline order (-skip db is the inverse)
go tool nano gen -list               # print the plan: step, package, inputs, outputs
go tool nano gen -json               # one event per line: {step, level, file, line, msg, fix}
go tool nano gen -diff               # with -check: the first 20 lines of each diff
go tool nano gen config -dir cmd/app # one generator, as today
```

`cmd/nano/main.go:run` requires `len(args) >= 2` today. The no-name form is a new route there.

**The project declaration.** One file per module, at a fixed path. It carries no build tag, so `go build ./...` and gopls type-check it; no binary imports it.

```go
// Package nanogen declares the inputs of this module's code generation that
// cannot be discovered. Run: go tool nano gen
package nanogen

import "github.com/nanostack-dev/nanostack-framework/gen/project"

var Project = project.Project{
	DB: &project.Postgres{
		Image:      "timescale/timescaledb:2.23.0-pg18", // a migration creates a hypertable
		Migrations: "migrations",
		Out:        "internal/db/gen/anchor",
		Types:      []project.TypeOverride{{DB: "text[]", Go: "github.com/lib/pq.StringArray"}},
	},
	API: &project.OpenAPI{
		Spec:   "cmd/http/openapi.yaml",
		Server: project.Package{Dir: "internal/api", File: "gen.go"},
	},
	Env: "../../.env.example",
}
```

- `nano` loads only this package (it imports only `gen/project`), so it type-checks even when every other package in the module is stale. A field typo is a compile error.
- It then reads the composite literal from the syntax tree. Values must be literals or constants. A function call or a variable is an error at file:line: `nanogen/project.go:9: project values must be literals; fix: replace imageFor("pg") with a string`.
- `Out` is explicit. The DB generator stops appending the database name to the path (`F/db/generator/postgres/postgres_generator.go:69`).
- `Env` keeps anchor's current target. Anchor writes `A-root/.env.example` today (`A/cmd/app/app.go:9`), and both that file and `A/.env.example` exist.
- A client is `project.Client{Dir, Package, PlainTypes: true}`. `PlainTypes` is a structured transform on the parsed kin-openapi document: drop `x-go-type` (except `string`) and `x-go-type-import`, and the `x-go-type-skip-optional-pointer` that comes with a nullable hint. It replaces `client-spec.sh` and its gitignored intermediate.
- Without `nanogen/project.go`, `nano gen` still runs the discovered steps, and `-list` prints a pre-filled declaration from the `migrations/` directory and `openapi.yaml` files it finds.

**Discovery.** By `go/parser`, never by type-checking, so it works on a broken tree:

| Step | Discovered from |
|---|---|
| errors | a `//nano:errors` interface (ADR 0022, unchanged) |
| mapper | a non-test file that calls `nanomap.Func` |
| config and wire | a package with a `//go:build nanogen` file that calls `nano.Manifest`; config runs on that package's `Config` |
| lint | `.golangci.local.yml` at the module root |

`testdata/` and `vendor/` are skipped.

**Steps.**

| # | Step | Outputs (anchor) | Needs | Loader |
|---|---|---|---|---|
| 1 | db | `internal/db/gen/anchor/public/{model,table,view}/*.go` (each table type with `UniqueKeys()`), `internal/db/gen/anchor/nanogen.sum` | Docker or `NANO_GEN_DATABASE_URL` | none |
| 2 | api | `internal/api/gen.go`, `internal/api/handlers.gen.go`, each declared client | the spec | none: kin-openapi plus oapi-codegen in-process |
| 3 | validate | `internal/api/validate.gen.go` | api | tolerant (`F/gen/validate/load.go:40-41`) |
| 4 | errors | `errors.gen.go`, `ERRORS.md` per catalogue | none | overlays its own output (`F/gen/errors/load.go:65-75`) |
| 5 | mapper | `mapper.gen.go` per declaring package | db, api, errors | stubs its own output, type-checks the rest |
| 6 | config | `cmd/app/config.gen.go`, `cmd/app/CONFIG.md`, `Env` | every step above | `-tags=nanogen`, stubs its own output |
| 7 | wire | `cmd/app/wire.gen.go` | config | `-tags=nanogen` (today) |
| 8 | lint | `.golangci.yml`, `.golangci-lint-version` | none | file only |

**Inside the api step**, the order is fixed by oapi-codegen's global state. `codegen.Generate` sets `globalState.options` (`codegen.go:157-161` in v2.8.2) and `OperationDefinitions` reads it (`operations.go:931,1263,1419`). So:

1. Load the spec once with `openapi3.Loader{IncludeOrigin: true}`.
2. Check the tag rules (Feature 3).
3. `codegen.Generate(spec, serverConfig)`.
4. `codegen.OperationDefinitions(spec)` while the server options are set, then render `handlers.gen.go`.
5. For each client, apply `PlainTypes` to a copy of the spec and call `codegen.Generate(clientSpec, clientConfig)`.

Steps 3 to 5 never run in parallel. `oapi.Config` and `oapi.DefaultServiceConfig` (`F/gen/oapi/config.go:31-75`, paths that match neither service) are deleted; the project declaration replaces them. nano prepends its header to oapi-codegen's output.

**Loads in `-check`.** Each generator's `Options` gains `Overlay map[string][]byte`. When step *k* loads packages:

- the fresh outputs of earlier steps are overlaid;
- an existing output of a later step is replaced by a **body-stripped stub**: every declaration stays, every function body becomes `panic("stub")`. `Build` and `Overrides` survive, so code that calls them still type-checks;
- a step may supply a **forward stub** for its outputs instead (D27 of the repo design). The repo step does: for each bound interface, by parsing alone, a method that panics for every interface method with no method of that name on the implementation, and no `var _` line. A body-stripped stub keeps the last run's `var _` line, which fails once the interface gains a method, and cannot add that method; the forward stub can;
- an output whose directory does not exist is reported `missing`. The steps that need it are marked `skipped: needs db`.

**The config load trade-off.** Under `-tags=nanogen`, no generator type-checks `A/cmd/app/run.go` or `E/cmd/app/app.go`. `go build` and `go vet` still do, and the CI build runs both. Both services keep `Config` in an untagged file (`A/cmd/app/app.go`, `E/cmd/app/config.go`), so config finds it under the tag.

**DB step.**

- Starts the declared image through testcontainers (`gen/db`, the server of the run, which the runner starts on first use and stops at the end). With `NANO_GEN_DATABASE_URL` set, it creates a scratch database on that server and drops it afterwards. Promoting `pg/internal/pgtest` and `db/tests/internal/testenv` to a public `db/dbtest` is left for later: it would put testcontainers in the graph of the `db` module, which services import at run time, and the step needs only a starter of one container.
- Applies `migrations/` in-process with `pg/nanomigrate` until ADR 0020's `db/migrate` exists. The `migrate` CLI, openssl and bash go away. This design does not adopt golang-migrate; `nanomigrate` is the existing bridge.
- Reads the schema with `postgres.ReadSchema` and renders it with `template.Render`, which returns the files in memory (`template.File`; the `db` module cannot import `gen`, which requires it) and never calls `os.RemoveAll`. No type override: the `text[]` hook that both `dbgen.go` copies carry never fires on the fork, which names an array column by its element type and already maps a one-dimensional `text[]` to `pq.StringArray`.
- Writes `UniqueKeys()` on each table type, from one `pg_index` query: the primary key and each unique index that is valid, not deferrable, without an expression key, with no predicate other than `<key column> IS NOT NULL`, and with no key column under a nondeterministic collation that the index does not use. Every other unique index is a `Not a key` comment with its reason.
- `nanogen.sum` records a sha256 of the inputs (migration names and contents, image, types, framework version) and of every output. The step skips the database when the input digest matches, every output matches its hash, and no unlisted generated file exists under `Out`. `-fresh` bypasses it. Without the cache every local run needs Docker, and `-skip db` would become the habit, hiding real DB drift until CI.
- Migrations changed and no Docker: exit 3, `fix: start Docker, or set NANO_GEN_DATABASE_URL`.

**Output.** A clean run prints nothing. Otherwise one line per file (`wrote internal/api/handlers.gen.go`, `stale …`, `missing …`, `orphan …`), then each error as `file:line: message` and an indented `fix:` line.

**What it replaces.**

| Today | End state |
|---|---|
| `generate_{anchor,echopoint}.sh`, both `dbgen.go`, the oapi lines in `tools.go`, the three `*codegen.yaml`, `scripts/client-spec.sh` | deleted |
| `//go:generate` lines for `nano gen config` and `wire` in `cmd/app` | deleted; the guard rejects them |
| `genconfig.Run(Check)` and `wire.Run(Check)` Go tests | deleted |
| CI | one dagger step per module: `GOWORK=off go tool nano gen -check -fresh` |
| TS clients (pnpm) | unchanged, dagger `@generate` |
| echopoint `integrationgen` | unchanged, its own `go:generate` line and scheduled drift job |

### Worked example: anchor, a renamed Config field

An agent renames `Config.Postgres` to `Config.Database` in `A/cmd/app/app.go`.

- Today: `nano gen config` loads `cmd/app` without tags, includes `wire.gen.go`, which reads `cfg.Postgres` (`wire.gen.go:650`), and refuses. `nano gen wire` loads a stale `config.gen.go` and refuses. The agent must delete files to recover.
- With the pipeline: config loads under `-tags=nanogen`, where `wire.gen.go` does not exist, and writes `config.gen.go`. Wire then loads the fresh file and writes `wire.gen.go`. One command, no manual step.
- `go tool nano gen -check` in CI on the same change without regeneration: `stale cmd/app/config.gen.go`, `stale cmd/app/CONFIG.md`, `stale cmd/app/wire.gen.go`, exit 1.

### Guardrails

- CI checks every Go output, including the oapi server, the clients and the DB models, for the first time.
- An agent cannot run steps in the wrong order or forget one.
- Orphans fail `-check`, so a deleted catalogue or mapper cannot leave generated code behind.
- An unknown `//nano:` directive and a `//go:generate` that bypasses the pipeline are errors.
- Exit 3 tells "my code is wrong" from "the machine lacks Docker".

### Errors an agent sees

```
nanogen/project.go:12: API.Spec cmd/http/openapi.yml does not exist
  fix: the module has cmd/http/openapi.yaml
internal/api/doc.go:3: unknown directive //nano:mapers
  fix: known directives are //nano:errors and //nano:error; mapper declarations need no marker
cmd/app/app.go:9: //go:generate runs nano gen config; the pipeline runs this step
  fix: delete the line; run go tool nano gen
nano gen db: migrations changed and no Docker daemon is reachable (exit 3)
  fix: start Docker, or set NANO_GEN_DATABASE_URL
nano gen api: oapi-codegen v2.9.0 resolved; nano templates are pinned to v2.8.2 (exit 3)
  fix: go get github.com/oapi-codegen/oapi-codegen/v2@v2.8.2
nano gen mapper: skipped: needs db
```

## Feature 2: generated mappers, `nano gen mapper`

### Design

**Declaration.** Declarations are Go calls to the declaration-only package `gen/mapper/nanomap`. They sit inside `func _() { … }`, which Go never calls, so a pointer path such as `api.CreateProductOrganizationRequestObject{}.Body.Name` never runs. The file has no build tag. `nanomap` imports only the standard library, so the cost in a service binary is one small package with no `init`.

```go
package mapper

import "github.com/nanostack-dev/nanostack-framework/gen/mapper/nanomap"

func _() {
	nanomap.Func[model.Users, auth.User]("UserToDomain").Reverse("UserToEntity")
}
```

The generator writes `mapper.gen.go` next to the declaration, with plain package functions named by the string argument. The string is the one untyped part: a duplicate name, or a name that clashes with a hand-written function, is an error at the declaration's line.

**Rules, per target field.** First a declared rule for that field, then the name match, then the type rule.

| Case | Rule |
|---|---|
| Names | Exact match first, then initialisms folded (`ID`/`Id`, `URL`, `HTTP`, `HTML`, `TLS`, `API`). Two candidates for one field is an error. |
| Same or assignable type | Copy. Includes `time.Time`, `uuid.UUID` and KSUID strings. |
| Named type → underlying type (enum → string) | Cast. |
| string → enum | **Fallible by default**: a generated switch over the type's constants, error on an unknown value. `nanomap.Cast(field, "<why>")` opts out. |
| enum → enum | By constant name. A source constant with no counterpart is an error. |
| `T` → `*T`, `T` → `Option[T]` | Copy the value. Treating the zero value as absent is never inferred; `nanomap.ZeroAsNil(field)` declares it. |
| `*T` → `T`, `Option[T]` → `T` | **Error** unless `nanomap.NilAsZero(field)` or `nanomap.Default(field, value)` is declared. |
| `*T` ↔ `Option[T]` | `functional.FromPtr` / `ToPtr`, generic, so no wrapper per type. |
| Different names | `nanomap.From(targetField, sourceField)`: the target field takes that source field, and the type rules of this table still apply to the pair. The source field counts as used. A target with two `From` rules, or with `From` and `Skip`, is an error. |
| Nested struct | The declared `Func` for that pair, or the hand-written function registered for it with `nanomap.Use(fn)`. None, or more than one, is an error that prints both fixes: the `nanomap.Func[...]` line and the `nanomap.Use(...)` line. Nothing is derived implicitly. |
| Slices and maps | Element by element. Nil stays nil unless `nanomap.NilAsEmpty(field)`. |
| jsonb ↔ struct | `nanomap.JSON(field)`. A decode failure returns an error that names the field path. This is the only policy. |
| string ↔ uuid | `nanomap.Via(field, uuid.Parse)`. |
| Custom | `nanomap.Via(field, fn)` with `fn` typed by generics: `func(S) D`, `func(S) (D, error)`, `func(context.Context, S) D` or `func(context.Context, S) (D, error)`. |
| Field not on the source | `nanomap.Arg(field, "name")` adds a parameter of the field's type, in declaration order. |
| Join row | `nanomap.Flatten(sourceField)` lifts a nested struct's fields. Flattening a pointer makes the function fallible. |
| Computed after copy | `.After(func(S, *D))` or `func(S, *D) error`; the fields it sets are listed with `nanomap.Computed(field)`. |
| Hand-written pair | `nanomap.Use(fn)`, at the top of `func _() { … }`, with `fn` of the same four shapes as `Via`. It registers `fn` for every nested field, slice element and map value of type `S` → `D` in the package's declarations. Its signature propagates like `Via`: a fallible `fn` makes each caller fallible, a `ctx` parameter makes each caller take `ctx`. |
| Unmapped target | Error. Waived only by `nanomap.Skip(field, "<why>")`. |
| Unused source | Error. Waived only by `nanomap.Unused(field, "<why>")`. |

**Signatures follow the rules.** Any fallible rule (fallible `Via`, `JSON`, string → enum, a flattened pointer) makes the generated function return `(D, error)`, and errors are wrapped as `UserToDomain: Status: …`. Any `Via` that takes a context makes the generated function take `ctx context.Context` first. Every call site stops compiling until it handles the change.

**Reverse is strict.** `.Reverse("Name", rules...)` inverts renames (`From` swaps its fields), casts, `Flatten` and enum mappings. A `Use` covers one direction: the reverse pair needs its own `Use` or `Func`. It never inverts `NilAsZero`, `Default`, `Arg`, `Via`, `After`, `Skip` or `Unused` on its own. Each such field needs its own rule in the `Reverse` call, or generation fails with a fix. MapStruct's `@InheritInverseConfiguration` drops such rules silently.

**Placement.**

- Anchor: domain↔entity in `internal/mapper`; api↔domain in `internal/api`, because the handlers there call those mappers and they need `api` types. That cycle is why `internal/api/*_mappers.go` exists.
- Echopoint: one declaration file per feature package, replacing `api_mapper.go` and `db_mapper.go`.
- Hand-written mappers stay where a rule cannot express them (echopoint `executions/db_mapper.go`, which takes a logger; the oneOf `TriggerMetadata` parse). A generated function calls them through `Via` for one field, or through `Use` for every field of that type pair.

**Generator contract.** `gen/mapper` has `Options{Dir, Check, Overlay}`, `Generate` returning `[]gen.File`, `Run` and `Main`. It loads the package without tags, stubs its own previous output, and type-checks what it generates before returning it.

### Worked example: anchor survey bug 1

The agent's first draft is the one line above. `nano gen` answers:

```
internal/mapper/mappers.go:6: UserToDomain (model.Users → auth.User): model.Users.Name is *string, auth.User.Name is string
  fix: nanomap.NilAsZero(auth.User{}.Name), or nanomap.Default(auth.User{}.Name, "<value>")
internal/mapper/mappers.go:6: UserToDomain: source field model.Users.Status is not used
  fix: map it, or nanomap.Unused(model.Users{}.Status, "<why>")
internal/mapper/mappers.go:6: UserToEntity (auth.User → model.Users): target field model.Users.Status has no source
  fix: nanomap.Arg(model.Users{}.Status, "status"), or nanomap.Skip(model.Users{}.Status, "<why>")
```

The third error is the half of bug 1 that writes `status = ''` on every insert. The name match maps `Name`, which fixes the other half. The agent applies the fixes, and the reverse states its own rule for `Name`:

```go
nanomap.Func[model.Users, auth.User]("UserToDomain",
	nanomap.NilAsZero(auth.User{}.Name),
	nanomap.Unused(model.Users{}.Status, "auth.User has no status yet"),
).Reverse("UserToEntity",
	nanomap.ZeroAsNil(model.Users{}.Name),
	nanomap.Arg(model.Users{}.Status, "status"),
)
```

Generated: `func UserToDomain(src model.Users) auth.User` and `func UserToEntity(src auth.User, status string) model.Users`. Every caller of `UserToEntity` must now pass a status, which forces the decision the hand-written mapper skipped.

**Enum and pointer rules: `A/internal/mapper/product_user_mapper.go`.** `model.ProductUsers.Status` is `string`; `user.ProductUser.Status` is `user.ProductUserStatus` with two constants, `ACTIVE` and `INACTIVE` (`A/internal/domain/product/user/user.go:12-17`). `product_users.status` is `VARCHAR(50) NOT NULL` with no CHECK constraint (`A/migrations/000001_init.up.sql:136`), so the database can hold other values.

```go
nanomap.Func[model.ProductUsers, user.ProductUser]("ProductUserToDomain",
	nanomap.NilAsZero(user.ProductUser{}.Name),
).Reverse("ProductUserToEntity",
	nanomap.ZeroAsNil(model.ProductUsers{}.Name), // today: OptionOf(name, name != "").ToPtr()
)
```

`ProductUserToDomain` returns `(user.ProductUser, error)`, because string → enum is fallible by default. Its reverse needs no rule for `Status`: enum → string is a plain cast. To keep today's unchecked cast, the declaration must say why: `nanomap.Cast(user.ProductUser{}.Status, "<reason>")`. Open question 2 asks which one anchor wants.

**Handler input: the 141 inline literals.** `A/internal/api/organization_api.go:111` builds `organization.CreateOrganizationInput` (`A/internal/domain/organization/input_types.go:16`) from `request.Body` (`*api.ProductOrganizationRequest`), the path parameter and two resolved values:

```go
// internal/api/mappers.go
func _() {
	nanomap.Func[api.ProductOrganizationRequest, organization.CreateOrganizationInput]("CreateOrganizationInput",
		nanomap.Arg(organization.CreateOrganizationInput{}.TenantID, "tenantID"),
		nanomap.Arg(organization.CreateOrganizationInput{}.ProductID, "productID"),
		nanomap.Arg(organization.CreateOrganizationInput{}.LicenseTemplateID, "licenseTemplateID"),
		nanomap.Via(organization.CreateOrganizationInput{}.Metadata, mapMetadataToInput),
		nanomap.Unused(api.ProductOrganizationRequest{}.FoundingMember, "CreateWithMember path"),
		nanomap.Unused(api.ProductOrganizationRequest{}.License, "resolved to licenseTemplateID by the handler"),
	)
}
```

`Name` and `Description` match by name and type. `Metadata` (`*api.Metadata`, a `map[string]interface{}`) goes through the existing `mapMetadataToInput`. The 8-line literal becomes `CreateOrganizationInput(*request.Body, tenantID, request.ProductId, licenseTemplateID)`.

### Guardrails

- A field added on either side fails generation until it is mapped or waived by name with a reason. On today's code this reports bugs 1, 2 (`ExpiresAt` has no source) and 3 (`ScheduleRunId`, `TimeoutSeconds`).
- A mapping that can fail, or needs a context, says so in its signature.
- A field rename through gopls updates the declarations, because they are compiled Go.
- Mappers leave the wiring graph: no provider, no struct.
- Optional lint rule (open question 4): flag composite literals of domain input types inside `internal/api`, so new handlers call declared mappers.

### Errors an agent sees

```
internal/api/mappers.go:14: CreateOrganizationInput: two sources for ProductID: Arg "productID" and api.ProductOrganizationRequest.ProductId
  fix: remove the Arg, or nanomap.Unused(api.ProductOrganizationRequest{}.ProductId, "<why>")
internal/mapper/mappers.go:21: ProductUserToEntity: NilAsZero(user.ProductUser.Name) cannot be inverted
  fix: nanomap.ZeroAsNil(model.ProductUsers{}.Name) in Reverse, or declare ProductUserToEntity as its own Func
internal/mapper/mappers.go:30: OrganizationToDomain: no Func for model.Licenses → license.License (field License)
  fix: nanomap.Func[model.Licenses, license.License]("LicenseToDomain")
internal/mapper/mappers.go:30: OrganizationToDomain: Unused(model.Organizations.DeletedAt) needs a reason
  fix: nanomap.Unused(model.Organizations{}.DeletedAt, "soft delete is read by the repository")
internal/mapper/mappers.go:6: UserToDomain is also declared at internal/mapper/user.go:12
  fix: rename one of them
```

## Feature 3: the API split by OpenAPI tag

### Design

**Generated, in the oapi package** (`internal/api` for anchor, `internal/apigen` for echopoint). The interfaces live there so echopoint's feature packages, which already import `apigen`, implement them without an import cycle.

```go
// Code generated by nano gen api. DO NOT EDIT.

// EmailServer serves the operations tagged Email.
type EmailServer interface {
	// (POST /v1/products/{product_id}/email/sends)
	SendEmail(ctx context.Context, request SendEmailRequestObject) (SendEmailResponseObject, error)
	// ... the other 14 Email operations
}

// ... one interface per tag (21 in anchor)

type Handlers struct {
	email     EmailServer
	licensing LicensingServer
	// ...
}

// NewHandlers refuses a nil handler and names its tag.
func NewHandlers(email EmailServer, licensing LicensingServer /* , ... */) (*Handlers, error)

func (h *Handlers) SendEmail(ctx context.Context, r SendEmailRequestObject) (SendEmailResponseObject, error) {
	return h.email.SendEmail(ctx, r)
}

// ... one delegator per operation

var _ StrictServerInterface = (*Handlers)(nil) // the flat upstream interface is untouched
```

- Tag names become Go names through oapi-codegen's `codegen.ToCamelCase` ("Status pages" → `StatusPagesServer`).
- `handlers.gen.go` grows with the API (about 190 delegators in echopoint). It is generated, so its size costs nothing to maintain.

**Hand-written handlers return their tag interface.**

```go
// A/internal/api/email_api.go
type emailHandler struct{ emails emailsvc.EmailService }

func NewEmailHandler(emails emailsvc.EmailService) EmailServer { return &emailHandler{emails} }
```

- A missing operation is a compile error in this file: `*emailHandler does not implement EmailServer (missing method SendEmail)`.
- The manifest lists `api.NewEmailHandler` and `api.NewHandlers` in `nano.Providers`. No `nano.Bind`. The HTTP server takes the concrete type today (`A/cmd/http/server.go:59` takes `*api.AnchorAPI`) and takes `*api.Handlers` after the split, so the router needs no Bind either. Wire accepts a function provider that returns an interface; nothing in `gen/wire` rejects it.
- Consequence: wire marks an interface-typed provider's lifecycle as dynamic (`F/gen/wire/checks.go:32`, "lifecycle by dynamic type" in `emit.go:296`). Handlers have no Start or Stop, so this costs nothing, but a handler cannot rely on static lifecycle checks.
- Consequence: the constructor returns an interface, against Go's "return structs" habit, and a handler test gets the interface. The handler type can be unexported.
- Each handler takes only its own services. `NewAPI` and its 31 parameters go away, and the two unused ones become visible.

**Spec rules**, checked in the api step before generation. Lines come from `openapi3.Origin.Key.Line` with `Loader.IncludeOrigin` set (kin-openapi v0.149.0, `openapi3/origin.go`, `loader.go:37`).

- Exactly one tag per operation.
- Every used tag is declared in the top-level `tags`, and every declared tag is used.
- No two tags produce the same Go name.

### Worked example: anchor

Anchor's tags already match its files, so the split needs no retagging. Today the spec declares 19 tags and uses 21: 6 are used but undeclared (Authentication, Email, PlatformUser, ProductResourceType, Resource, User) and 4 are declared but unused (Membership, Organization, Permissions, PlatformAuth). Those 10 spec fixes come first.

The split then runs in two moves:

1. **Compile at once.** 21 one-line constructors that return the existing `*AnchorAPI` as each tag interface, for example `func NewEmailServer(a *AnchorAPI) EmailServer { return a }`. `NewAPI` survives for now. The build is green.
2. **One file at a time.** Replace one constructor with a real handler (`emailHandler` above) that takes only `emailsvc.EmailService`, and move its methods. `product_api.go` splits into Product and ProductUser handlers; `platform_user_api.go` into PlatformUser and User. Licensing stays one tag: `licensingHandler` keeps 21 methods across its 6 existing files. When the last constructor is replaced, `NewAPI` and `AnchorAPI` are deleted.

### Guardrails

- Every operation has exactly one handler. A missing method fails to compile in the handler's file; a nil handler fails at startup with its tag name.
- Clashes and silent shadowing cannot happen: each tag is a named field.
- Each handler gets only its own dependencies, so dead ones show.
- The tag rules make the spec's grouping a checked contract.

### Errors an agent sees

Line numbers below are placeholders; the real ones come from the spec.

```
cmd/http/openapi.yaml:<line>: operation SendEmail has no tag
  fix: add one of the declared tags: Access, Email, Integration, Licensing, ...
cmd/http/openapi.yaml:<line>: operation ListEmailSends has 2 tags (Email, Product); a tag is a handler boundary
  fix: keep one tag
cmd/http/openapi.yaml:<line>: tag Email is used by 15 operations but not declared
  fix: add "- name: Email" under the top-level tags
cmd/http/openapi.yaml:<line>: tag Membership is declared but no operation uses it
  fix: delete it, or tag an operation with it
cmd/http/openapi.yaml:<line>: tags "Status pages" and "StatusPages" both become StatusPagesServer
  fix: rename one tag
internal/api/email_api.go:14: *emailHandler does not implement EmailServer (missing method SendEmail)
  fix: func (h *emailHandler) SendEmail(ctx context.Context, request SendEmailRequestObject) (SendEmailResponseObject, error)
nano gen wire: NewHandlers: no provider for api.EmailServer
  fix: add a provider that returns api.EmailServer to nano.Providers in cmd/app/manifest.go
```

The `fix:` line on the compile error is new. Go's message stops at the method name; the pipeline adds the full signature when the missing method belongs to a generated tag interface. Wire's existing Bind check (`F/gen/wire/items.go:253-262`) and its unbound-interface hint (`F/gen/wire/graph.go:268-280`) stay; only the full-signature fix text is added to them.

## Pipeline order

```
 nanogen/project.go ──(static read)──┐      markers and declarations ──(go/parser)──┐
                                     ▼                                              ▼
                               ┌──────────────────── plan (nano gen -list) ────────────────────┐
                               ▼                                                                │
 1 db        container or NANO_GEN_DATABASE_URL, migrate, render ──► models, nanogen.sum         │
 2 api       load spec ─► tag rules ─► Generate(server) ─► OperationDefinitions ─► handlers.gen.go
             └─► for each client: PlainTypes ─► Generate(client) ──► client.gen.go               │
 3 validate  needs api ──► validate.gen.go         (a gap is reported; later steps still run)   │
 4 errors    each //nano:errors ──► errors.gen.go, ERRORS.md                                     │
   mapper    (part 2) needs db, api, errors; each nanomap package ──► mapper.gen.go              │
 5 repo      needs db; each nanorepo package ──► repo_base.gen.go (go/parser reads UniqueKeys)   │
 6 config    -tags=nanogen; needs 3-5 ──► config.gen.go, CONFIG.md, Env                          │
 7 wire      -tags=nanogen; needs config ──► wire.gen.go                                         │
   lint      (item 11) independent ──► .golangci.yml, .golangci-lint-version                     │
                               │                                                                │
                               ▼                                                                │
                         orphan scan ─► report (text or -json) ─► exit 0 / 1 / 2 / 3 ◄──────────┘

 generate: each step writes before the next loads        -check: outputs stay in memory as overlays
 a failed step skips only the steps that need it         later outputs are body-stripped stubs
```

### How the repo step resolves an interface method

For each method of a bound repository interface, the first rule that applies wins ([search design](search.md), S10):

1. **Hand-written.** A method of the implementation, written by hand or promoted from an embedded field, wins and must match the interface's signature. A hand-written method that takes a declared search starts from `searchexec.Resolve`, `Run` or `RunPage` with its own `ctx`, the search's spec variable and `q.Query`, and never assigns `q` before the call (E19). A promoted method that takes a declared search fails (E19): write it on the implementation.
2. **Derived.** A name of the grammar (`FindBy`, `GetBy`, `ExistsBy`, `CountBy`, `DeleteBy`, `FindAll`, `Count`) becomes one call to a base helper. A derived method takes column values, never a search builder.
3. **Search.** A method with the parameters `(context.Context, XSearch)`, for a search declared in the same package, and the results `(search.Result[D], error)` becomes one call to the base hatch `search` or `searchPage`. `D` is the output of the search's mapper, or `Row` with no mapper. The name is free, and two methods may take one search.
4. **Else** the run fails at the method (E20) and prints the signature to paste.

A declared search that no method takes fails the run (E24), in a package that binds a repository interface. `nano gen repo -explain` prints the rule that each method took.

## Implementation plan

Each item is one PR in the framework, in this order. Each ships its ADR or doc change with it.

| # | PR | Content | Tests |
|---|---|---|---|
| 1 | Nested `gen` module | Move `gen/...` and `cmd/nano` into a nested `gen` module (roadmap item 3), so `x/tools`, oapi-codegen and testcontainers stay out of the runtime root module. Document the `tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano` directive. | `internal/layers` passes with `gen` and `cmd/nano` gone from the root module; the root `go.mod` no longer requires `golang.org/x/tools`; `go mod tidy` diff is empty in both modules; CI's existing `nano gen config|wire -check` on `internal/nanoexample` still passes. |
| 2 | Contract change | `gen.Error{File, Line, Msg, Fix}`; exit code 3; `Overlay` in `Options` of config, errors, validate and wire; config loads with `-tags=nanogen`. Update the contract text in `F/gen/gen.go` and `F/gen/README.md`. | A fixture with a renamed Config field and a `wire.gen.go` that reads the old one: config succeeds (the deadlock is gone). A table test that every error path of the four generators sets `Fix`. Overlay test per generator. |
| 3 | Pipeline core | `gen/project` and its static reader; discovery; fixed order; generate-writes-per-step and `-check` overlays with body-stripped stubs; header orphans; `-check -diff -json -only -skip -list`; the unknown-directive and `go:generate` guards; the no-name route in `cmd/nano/main.go`; validate waivers move to `x-nano-unenforced`. Covers config, errors, validate and wire. `internal/nanoexample` gets `nanogen/project.go`; framework CI runs `nano gen -check`. ADR 0023 (pipeline). | Golden runs over testdata modules: a stale config plus a stale wire; an orphan after a deleted catalogue; an unknown `//nano:` directive; a forbidden `//go:generate`; a validate gap that lets wire still run and exits 1; a non-literal project value; `-json` event schema; `main_test.go` for `nano gen` and `nano gen -list`. |
| 4 | api step | oapi-codegen v2.8.2 in-process with `oapitemplates` and `std-http-server`; the build-info version check; `PlainTypes`; nano headers; delete `oapi.Config` and `DefaultServiceConfig`. | Byte-equal golden against `go run …/oapi-codegen@v2.8.2` on `F/http/nanohttp/internal/example`; `PlainTypes` output equals `client-spec.sh` output on a fixture; version-check test with a fake build info. |
| 5 | Tag split | Tag rules with spec lines; `handlers.gen.go`; unexported `Handlers` fields; the full-signature fix on missing-method errors and in wire's Bind message. ADR 0024 (tag split). | Goldens for untagged, multi-tagged, undeclared, unused and Go-name-collision specs; a compile test that `*Handlers` satisfies `StrictServerInterface`; a test that a server run then a client run give the same `handlers.gen.go` as a server run alone (globalState order); `NewHandlers` nil test. |
| 6 | db step | `template.Render` and `postgres.ReadSchema` in `db/generator`, in memory, with the nano header; `UniqueKeys()` on each table type; in-process `nanomigrate`; `NANO_GEN_DATABASE_URL`; `nanogen.sum`; `-fresh`; exit 3; the orphan fix (the db unit plans the files `nanogen.sum` lists, and a failed step deletes nothing). Services add `nano gen -check -fresh` to CI with their db-step adoption. | testcontainers run on a migrations fixture (TimescaleDB image) with one unique index per shape; the key rule over 15 shapes; the keys against the live catalogue; cache hit, a miss for each changed input or output, `-fresh`; no Docker gives exit 3 with the fix; a dropped table becomes an orphan; a dropped key makes `-check -fresh` fail. |
| 7 | repo base (landed) | `db/nanorepo` declarations; the generated base per table; `transactor.ExecAffected` and `ErrMoreThanOneRow`; `Postgres.Scopes` and the hand-written scope warning. ADR 0026 (`nano gen repo`). | Base goldens; database tests for every helper and the lock rules. |
| 8 | derived names and resolution (landed) | Resolution, the derived grammar, the unique-key proof read with `go/parser`, `<file>_repo.gen.go` with `var _`, the forward stub, `-explain`. | The prototype's error and `-explain` goldens; backstop, stale-output and drift cases; database tests against hand-written methods. |
| 9 | mapper v1 (part 2) | `gen/mapper` and `nanomap`: name matching with initialisms, copy and cast, pointer and `Option` rules, string→enum switch and `Cast`, enum→enum, nested `Func` lookup, slices and maps, `Skip`/`Unused` with reasons, `Arg`, `From`, `Flatten`, `Via` and `Use` (with error and ctx propagation), strict `Reverse`. Rename ADR 0015's row-scan mappers to scanners. ADR 0025 (mappers). The repo step then runs after mapper. | Goldens per rule; a fixture equal to anchor's `user_mapper.go` that prints the three errors above; reverse of every rule kind (invertible ones generate, the rest fail with fixes); the generated code type-checks; a test that every error has a `Fix`; a gopls-free rename test: renaming a field breaks `go build` of the declaration. |
| 10 | mapper v2 | `JSON`, `After` and `Computed`, `NilAsEmpty`, duplicate-name checks. | `JSON` decode failure returns an error with the field path; `After` signature check. |
| 11 | lint step | The pipeline calls the `lintconfig` library as its last step. `lint/cmd/lintconfig` stays for repositories that do not use the pipeline. | Existing `lintconfig -check` cases run through the pipeline. |
| 12 | Docs | `docs/generation.md` describes the pipeline; roadmap item 2 closes; decision-log entries for P1-P12, M1-M7 and T1-T5. | Docs link check. |

Part 1 is items 1 to 8, with `nano gen repo` in 6 to 8 (`reports/design-repo.md` in the workspace). Mapper moved to part 2.

## Migration impact on anchor and echopoint

Nothing is changed now: other agents own those worktrees. Each item below is a later PR in that repository, after the framework PRs it needs.

**Both services, in this order.**

1. **chi-server to `std-http-server` with the nano templates** (ADR 0019), as its own PR before the pipeline. Routing and 404/405 bodies can change. Before merge, export the product's gate flows from the prod Echopoint CI org and grep them for any changed status or body (AGENTS.md cross-repo rule). `validate` then runs on the service for the first time; expect `x-nano-unenforced` entries.
2. **Pipeline adoption.** Add `nanogen/project.go` and the `tool` directive. Delete the scripts, `dbgen.go`, the oapi lines in `tools.go`, the `*codegen.yaml` files, the `go:generate` lines in `cmd/app` and the `Run(Check)` tests. Today's oapi outputs carry oapi-codegen's header, not nano's, so the first run rewrites them in place; files that the run no longer produces (anchor `clients/go/types.gen.go` if the client becomes one file) are deleted by hand once. CI runs `GOWORK=off go tool nano gen -check -fresh`; the dagger step needs a Docker socket or a database service passed as `NANO_GEN_DATABASE_URL`. With `Out` set to the current path, DB model imports do not change.
3. **Tag split** (after framework PR 5).
4. **Mappers, one package at a time** (after framework PR 7).

**Anchor.**

- `A-root/clients/go` gets its own `nanogen/project.go` with `Spec: "../../apps/anchor/cmd/http/openapi.yaml"` and `PlainTypes: true`, and its own CI check.
- 10 spec tag fixes, then the two-move split above. No retagging.
- `internal/mapper` first. The strict policy surfaces bug 1 (`Users.Name`, `Users.Status`), bug 2 (`ExpiresAt`) and the `ProductUserStatus` question. The 141 inline handler literals move to declared mappers over time.
- The `XxxMapper` structs in `internal/mapper` and `mapper.Module` leave the manifest; services lose their mapper parameters.

**Echopoint.**

- **Retag before the split.** 8 tags span several handlers (for example Flows → flows 22, executions 10, variables 4, API 1; Collections → collections 19, openapisyncs 10), and "Organization" (10 variables operations) sits next to "Organizations". operationIds, statuses and bodies do not change, so gate flows are unaffected.
- The TS client is not affected by retagging. `E/apps/frontend/openapi-ts.config.ts` has no tag grouping, and `sdk.gen.ts` exports one function per operation (189), named by operationId. The public docs grouping does change.
- The 2 operations on `EchopointAPI` itself (LaunchFlow, CompleteEphemeralExecution) move into handlers. The 5 missing-method mismatches the tag study found become compile errors in the handler files, by design.
- The interfaces live in `apigen`; feature packages already import it.
- 213 mapper functions convert per feature. JSON decode failures now return errors; `executions/db_mapper.go` (log and drop) stays hand-written until open question 3 is answered. `cmd/it/ct`'s client loses its domain `x-go-type` hints through `PlainTypes`, so CT tests exercise the wire contract with plain types.
- `integrationgen` keeps its `go:generate` line and drift job.

## Open questions for Alexis

1. **Mapper declaration files without a build tag.** Untagged files give compile errors, `go vet` and gopls renames by default, at the cost of linking the stdlib-only `nanomap` package into service binaries. The alternative is `//go:build nanogen`, like `manifest.go`, plus a gopls `buildFlags` snippet in every repo. **Recommendation: untagged.** The manifest is tagged because its initializer references every provider; a `func _()` body never runs, so that reason does not apply here.
2. **Anchor `ProductUserStatus` from the database.** The column has no CHECK constraint and the domain has two constants. The default makes `ProductUserToDomain` fallible and rejects unknown values; `Cast` keeps today's unchecked read. **Recommendation: keep the fallible default and add a CHECK constraint migration**, so an invalid status fails at write time instead of read time.
3. **One JSON decode policy.** Returning the error changes echopoint executions, which log and drop a bad stored value today. **Recommendation: return the error everywhere**, and keep `executions/db_mapper.go` hand-written until its stored data is known to decode.
4. **Lint rule against inline domain inputs in `internal/api`.** A rule that flags composite literals of domain input types in handlers keeps new code on declared mappers. **Recommendation: add it per service after its literals are migrated**, not before, so it never lands as a wall of waivers.
