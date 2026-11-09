# Generated code: a reviewer's guide

This guide covers every file the framework generates, or will generate, for a service. For each generator it shows what you write, what comes out, how to read a diff of it, which guardrails apply, and what you never read by hand. The aim is that you review the few lines a developer wrote and trust checks for the thousands of lines a generator wrote.

**Sources.** Framework paths are relative to this repository at `main` (`4b6c468`, after #69). Service paths start with the service name: anchor means `apps/anchor` in the anchor repository at `7141b9e9`, and echopoint means the echopoint repository at `a9f8780f`. Both services are on their nano branches and pin `v0.23.0-beta.1`. The pipeline, the mapper generator and the tag split come from [the `nano gen` pipeline design](design/nano-gen-pipeline.md) ("design" below, with its line numbers). Prototypes live in the workspace `reports/` folder, outside this repository.

**Excerpts.** Each excerpt matches its file apart from trimming, which a `// …` (or `# …`, `<!-- … -->`) line marks. A comment at the end of a code line that explains it (for example `// nano: replaces …` or `// ToDomain: NULL -> ""`) is this guide's, not the file's.

**Status vocabulary.**

| Status | Meaning |
| --- | --- |
| **shipped** | In `v0.23.0-beta.1`, and anchor and echopoint run it. |
| **shipped, unused** | In `v0.23.0-beta.1`, but no service runs it yet. |
| **on main** | Merged after `v0.23.0-beta.1`. No release that the services pin has it. |
| **in progress** | Being implemented on a framework branch. Not merged, no PR yet. |
| **designed** | Specified in the design. No code yet. |
| **prototype** | Throwaway code in the workspace `reports/prototypes/`. Not for merge. |

PR 2 of the pipeline (#69) merged at `4b6c468`. PR 3 (pipeline core) is **in progress** on a framework branch, not yet pushed. It has started with the project declaration and with validate waivers as `x-nano-unenforced`. Mapper (PRs 7-8) and the tag split (PR 5) have no code yet.

## Summary

| Generator | Status | You write | It generates | Lines you write → lines generated | Guardrail |
| --- | --- | --- | --- | --- | --- |
| db (go-jet fork) | **shipped** (pointer mode). `Option[T]` mode shipped, unused. `nano gen` db step with `UniqueKeys` and `nanogen.sum`: **on main**, unused. | SQL migrations, `dbgen.go` (the `Postgres` declaration with the db step) | `internal/db/gen/<svc>/public/{table,model,view}/*.go` | anchor: 1,652 SQL lines in 73 up migrations + 186 → 5,311 in 88 files. echopoint: 2,834 in 97 + 198 → 6,257 in 96 files. | Today none: no `-check`, and CI does not regenerate. With the db step: `nano gen -check -fresh` in CI. |
| api (oapi-codegen) | Upstream v2.8.0 **shipped** in the services. nano templates **shipped, unused**. In-process step **designed**. | `openapi.yaml`, `codegen.yaml` | `gen.go`: models, router, strict server | anchor: 10,424 + 7 → 26,184. echopoint: 16,442 + 7 → 44,625. | None in CI today. |
| validate | **shipped, unused**. One error per gap with its `-allow` fix: **on main**. Waivers as `x-nano-unenforced` in the spec: **in progress**. | `openapi.yaml` | `validate.gen.go` | 0 in the services. Framework example: 135 → 279. | `-check`. The nano template calls the validators, so a missing file does not compile. An unacknowledged gap exits 1. |
| errors | **shipped, unused** | One `//nano:errors` Go interface | `errors.gen.go`, `ERRORS.md` | 0 in the services. Golden fixture: 34 → 268 + 55. | `-check`. The generator type-checks its output. |
| mapper | **designed** | `nanomap.Func` declarations | `mapper.gen.go` | Hand-written today: anchor 147 functions in 2,345 lines, echopoint 213 in 3,965. Generated: 0. | Designed: a lossy conversion is an error unless declared. `-check`. |
| tag split | **designed** | One tag per operation in `openapi.yaml`, one handler per tag | `handlers.gen.go` | anchor: 133 operations in one interface today, 21 tags. | Designed: a compile-time assertion and `-check`. |
| config | **shipped**. Load under `-tags=nanogen`: **on main**. | `Config` struct and field tags | `config.gen.go`, `CONFIG.md`, `.env.example` | echopoint: 80 + module sections → 526 + 79 + 250. anchor: 46 + 65 + module sections → 421 + 66 + 198. | A `Run(Check)` test in each service. Bind reports every issue at start. |
| wire | **shipped**. Two-line `fix:` errors: **on main**. | `manifest.go`, each package's `module.go` | `wire.gen.go` | anchor: 56 → 2,218. echopoint: 98 → 2,758. | A `Run(Check)` test in each service. `file:line` errors with a fix. |
| pipeline | Nested `gen` module (#68) and error contract (#69) **on main**. `go tool nano gen` core (PR 3): **in progress**. Later steps **designed**. | `nanogen/project.go` | Runs all of the above in order | n/a | Designed: `-check` covers every output, orphans included. |
| repo | **on main**, unused | One repository interface and one `nanorepo.Implement` per repository, `Postgres.Scopes` | `repo_base.gen.go`: per table, typed predicates and helpers on the transactor; `<file>_repo.gen.go`: the derived methods and a `var _` line per binding | Fixture: 3 declarations → 1,124 lines of bases; 11 derived methods → 117 lines. Prototype on anchor: 9 methods (156 hand lines) → 8 derived and 1 hand-written. | `-check`; an interface method that is neither hand-written nor derivable fails the run at its line; `var _` fails the build; `findOne` fails on two rows; a scope warning on a hand-written finder without the tenant column. |
| search | **on main**, unused | One `nanorepo.Search` declaration per search and one interface method that takes its builder | In `<file>_repo.gen.go`: the typed builder, the sort type, the spec, the repository method and the API mapper; `nanogen/search-exemptions.txt` | n/a | The tenant coverage proof, the API both-ways check and `-check`. [Searches](#searches). |

## How to review generated code

1. **Review the source of truth, not the output.** Each section below names the file a developer writes. A diff in generated code is explained by a diff in that file. If the generated diff has no source diff, the generator or its version changed. Ask why.
2. **Trust `-check` for freshness.** Each nano generator takes `-check`. It writes nothing and exits 1 when a committed output is missing or differs from what the source produces. Today the services run it for config and wire only, as a Go test (`Run(Options{Check: true})`). Once a service adopts the pipeline, CI runs `go tool nano gen -check -fresh` instead. `-check` proves that the output matches the source. It does not prove that the generator is right: the generator's golden tests do that. So read only the parts the checklists below name, and read more when the generator version changed.
3. **Know what is not checked.** The DB models and the oapi server have no freshness check today. For those, confirm that the generated diff matches the source diff (see their sections).
4. **Read the human-facing outputs.** Some outputs are written for people: `ERRORS.md` (the error contract), `CONFIG.md` (every setting) and the SQL comments of derived repository methods (`nano gen repo -explain` prints them all). A one-row diff there is the fastest way to see a contract change.
5. **Never hand-edit a file with a generated header.** The next run overwrites the edit, and, where a `-check` runs, it fails until the source matches. Change the source and regenerate.
6. **Read the waivers.** Every place the generator lets a developer opt out carries a reason or a flag: `-allow` in validate, `Skip`, `Unused` and `Cast` in a mapper, `config:"-"` in config. These are the decisions in the change. Check that each reason is true.

**Headers.** Every generated file starts with one of these lines. Do not review a file that has one, except for the items the checklists name.

| Generator | Header |
| --- | --- |
| db | `// Code generated by go-jet DO NOT EDIT.` from `dbgen.go`; `// Code generated by nano gen db. DO NOT EDIT.` from the db step (`# ` in `nanogen.sum`) |
| api | `// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.8.0 DO NOT EDIT.` |
| validate | `// Code generated by nano gen validate from openapi.yaml. DO NOT EDIT.` |
| errors | `// Code generated by nano gen errors from errors.go. DO NOT EDIT.` (`ERRORS.md` has it as an HTML comment) |
| config | `// Code generated by nano gen config from <root file>. DO NOT EDIT.` (`app.go` in anchor, `config.go` in echopoint; `#` in `.env.example`, an HTML comment in `CONFIG.md`) |
| wire | `// Code generated by nano gen wire from cmd/app/manifest.go. DO NOT EDIT.` |
| designed steps | `Code generated by nano gen <step>. DO NOT EDIT.` (design line 60) |

**How the generators run.** The services pin `v0.23.0-beta.1` and run `go run github.com/nanostack-dev/nanostack-framework/cmd/nano gen …` from `//go:generate` lines. On `main` the CLI moved to the nested `gen` module (#68): a service pins it with `go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>` and runs `go tool nano gen <name>`. See [Checking generated files](generation.md#checking-generated-files).

**Error format.** On `main` (#69), every generator error is a `*gen.Error{File, Line, Msg, Fix}`. It prints as the position and message, then an indented `fix:` line. Exit codes: 0 success, 1 a source error or stale output, 2 bad usage, 3 the environment failed. `v0.23.0-beta.1` printed the fix on the same line, after a `;`. This guide shows the `main` format.

```text
nano.go:10: no provider for missing.Store (needed by missing.NewService at providers.go:20)
  fix: bind an implementation with nano.Bind[missing.Store, *missing.PGStore]()
```

---

## 1. Database models: the go-jet fork (db)

**Status: shipped** in pointer mode (`*T` for a NULL column). The `functional.Option[T]` mode is **shipped, unused**: neither service's `db/gen` tree contains `functional.Option`. The pipeline's db step (`-check -fresh`, `nanogen.sum`, `UniqueKeys`, exit 3 without Docker) is **on main**; no service runs it yet. Guide: [nano gen db](generation.md#database-nano-gen-db). Decision: [ADR 0015](adr/0015-fork-go-jet-as-the-db-module.md).

### What you write

The SQL migrations, and one type hook per service in `dbgen.go`.

anchor `migrations/000001_init.up.sql:131-140` and `migrations/000005_integration_base.up.sql:70-71`:

```sql
CREATE TABLE product_users (
	id VARCHAR(255) PRIMARY KEY,
    product_id VARCHAR(255) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(100),
    status VARCHAR(50) NOT NULL, -- Default removed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (product_id, email)
);
-- … migration 5:
ALTER TABLE product_users
    ADD COLUMN external_id VARCHAR(255);
```

anchor `dbgen.go` (186 lines) and echopoint `dbgen.go` (198 lines) are near-copies. Their only customisation is `useArray` (anchor `dbgen.go:34-38`):

```go
func useArray(f *template.TableModelField, _ metadata.Table, c metadata.Column) {
	if c.DataType.Name == "text[]" {
		f.Type = template.NewType(pq.StringArray{})
	}
}
```

anchor `generate_anchor.sh` and echopoint `generate_echopoint.sh` start `timescale/timescaledb:2.23.0-pg18` in Docker, apply every migration, then run `go run dbgen.go`. The generator reads the live database, so it sees the **final schema**, not each migration.

### What is generated

| Service | `table/` | `model/` | `view/` | Total |
| --- | --- | --- | --- | --- |
| anchor `internal/db/gen/anchor/public` | 41 files, 4,013 lines | 43, 985 | 4, 313 | 88, 5,311 |
| echopoint `internal/db/gen/echopoint/public` | 38, 4,056 | 47, 1,218 | 11, 983 | 96, 6,257 |

Each table or view gets one `table/` (or `view/`) file and one `model/` file. Nullable `*T` fields: 79 in anchor, 180 in echopoint.

The model, all of it. anchor `internal/db/gen/anchor/public/model/product_users.go` (23 lines):

```go
//
// Code generated by go-jet DO NOT EDIT.
// …
type ProductUsers struct {
	ID         string `sql:"primary_key"`
	ProductID  string
	Email      string
	Name       *string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ExternalID *string
}
```

`NOT NULL` gives `T`. `NULL` gives `*T`. `PRIMARY KEY` gets `sql:"primary_key"`. Fields follow the database column order, so `ExternalID` (migration 5) comes last.

The SQL builder. anchor `internal/db/gen/anchor/public/table/product_users.go` (99 lines):

```go
var ProductUsers = newProductUsersTable("public", "product_users", "")

type productUsersTable struct {
	postgres.Table

	// Columns
	ID         postgres.ColumnString
	ProductID  postgres.ColumnString
	Email      postgres.ColumnString
	Name       postgres.ColumnString
	// … Status (ColumnString), CreatedAt and UpdatedAt (ColumnTimestampz)
	ExternalID postgres.ColumnString

	AllColumns     postgres.ColumnList
	MutableColumns postgres.ColumnList
	DefaultColumns postgres.ColumnList
}
// … AS, FromSchema, WithPrefix, WithSuffix and the constructor: the same shape in every table file

		allColumns       = postgres.ColumnList{IDColumn, ProductIDColumn, EmailColumn, NameColumn, StatusColumn, CreatedAtColumn, UpdatedAtColumn, ExternalIDColumn}
		mutableColumns   = postgres.ColumnList{ProductIDColumn, EmailColumn, NameColumn, StatusColumn, CreatedAtColumn, UpdatedAtColumn, ExternalIDColumn}
		defaultColumns   = postgres.ColumnList{CreatedAtColumn, UpdatedAtColumn}
```

- `MutableColumns` is `AllColumns` without the primary key. `DefaultColumns` lists the columns with a SQL `DEFAULT`.
- Column types do not carry nullability: `Name` is a `ColumnString`, like `Email`.

With the db step, each table file also ends with `UniqueKeys`: the primary key and every unique index that proves "at most one row", and a `Not a key` line for each other unique index ([the rule](generation.md#database-nano-gen-db)). For anchor's `product_users`, as the derived-repository prototype emitted it from the live catalogue:

```go
func (ProductUsersTable) UniqueKeys() map[string][]string {
	return map[string][]string{
		"product_users_pkey":                 {"id"},
		"idx_product_users_external_lookup":  {"product_id", "external_id"}, // WHERE (external_id IS NOT NULL)
		"product_users_product_id_email_key": {"product_id", "email"},
	}
}
```

After `UniqueKeys` come `ColumnTypes`, the catalogue type of each column by name (`text`, `inet`, `jsonb`, an enum's name), and `ForeignKeys`, the foreign keys of the table with their columns in key order ([the methods](generation.md#database-nano-gen-db)). A migration that changes a column type or a foreign key changes them; review them against the migration.

`nanogen.sum` sits next to the schema directory. It changes with every migration and every output; read its `input migration` lines against the migrations of the diff, and do not review its hashes.

The `Option[T]` mode, from the fork's test data. `db/tests/.gentestdata/option/jetdb/test_sample/model/link.go` (20 lines):

```go
type Link struct {
	ID          int64                     `sql:"primary_key"` // this is link id
	URL         string                    // link url
	Name        string                    // Unicode characters comment ₲鬼佬℧⇄↻
	Description functional.Option[string] // '"Z\%_
}
```

A service turns it on with `template.DefaultWith(postgres.Dialect, template.NullableOption)` (`db/generator/template/nullable.go:70-75`). For anchor that changes 79 fields and every mapper that reads them.

### How the code uses it

The `table` package builds the statement. `pkg/db/transactor` runs it and scans into `model.X`. A mapper converts `model.X` to the domain type. anchor `internal/repository/product_user_repository.go` (368 lines):

```go
func productUsersUpdatableColumns() postgres.ColumnList {
	return table.ProductUsers.AllColumns.Except(
		table.ProductUsers.CreatedAt,
		table.ProductUsers.UpdatedAt,
	)
}
// …
	stmt := table.ProductUsers.INSERT(
		productUsersUpdatableColumns(),
	).MODEL(entity).RETURNING(table.ProductUsers.AllColumns)

	return transactor.QueryMap(
		ctx,
		r.db,
		stmt,
		r.productUserMapper.ToDomain,
	).OnUnique(ErrProductUserEmailExists, productUserEmailUniqueConstraint).
		OnUnique(ErrProductUserExternalIDExists, productUserExternalIDUniqueConstraint).
		Value()
```

Lines 27-32 derive the write column list; nobody lists it by hand. Lines 168-179 write through `MODEL(entity)`, and a unique constraint becomes a domain error.

Nullability becomes a domain decision in the hand-written mapper (anchor `internal/mapper/product_user_mapper.go`, 46 lines, lines 19 and 34):

```go
name := functional.FromPtr(entity.Name).OrElse("")                    // ToDomain: NULL -> ""
name := functional.OptionOf(domain.Name, domain.Name != "").ToPtr()   // ToEntity: "" -> NULL
```

### Review a model diff after a migration

Review the migration. Then confirm that the generated diff matches it.

1. **Pairs.** `table/<t>.go` and `model/<t>.go` change together. If a migration changed and `internal/db/gen` did not, generation was not run. Nothing else catches this today.
2. **Position.** `ADD COLUMN` adds the field at the end of the struct and of `AllColumns`. A field in the middle, or a reordered struct, means a table was dropped and recreated. Check the migration for data loss.
3. **Nullability.** Every `*T`↔`T` flip breaks the build in the mappers that read the field. That is good. Check how each mapper resolves it, and look for silent defaults like `OrElse("")`.
4. **NOT NULL without DEFAULT.** The column joins `MutableColumns` but not `DefaultColumns`. With a derived list like `AllColumns.Except(...)`, a mapper that never sets the field writes `''` without an error. The design found this bug in anchor `users.status` (`internal/mapper/user_mapper.go`).
5. **Renames.** A renamed column is a removed field plus an added field. A `table.X.Old` reference fails to compile. A `MODEL(entity)` write through a derived column list does not. Look for a stale mapper assignment.
6. **Types.** `VARCHAR`/`TEXT` → `string`/`ColumnString`. `TIMESTAMPTZ` → `time.Time`/`ColumnTimestampz`. A one-dimensional `text[]` → `pq.StringArray` by the fork's default; `useArray` never fires, because the catalogue names an array column by its element type (`text`). An enum-like `VARCHAR` stays `string`; the mapper converts it.
7. **New unique or foreign-key constraints.** Check that the repository maps them with `OnUnique` or `OnForeignKey`. With the db step, a unique index added or dropped changes `UniqueKeys` of its table: a dropped key, or one that became partial, is a single-row derived method that loses its proof.

**Do not review by hand:** the alias helpers, `EXCLUDED`, the struct literal in `new<T>TableImpl`, `*_use_schema.go`, and `view/` files whose view did not change.

**With the db step (on main, unused):** `go tool nano gen -check -fresh` reports every stale, missing or orphaned file under `internal/db/gen`. Without Docker it exits 3 with `fix: start Docker, or set NANO_GEN_DATABASE_URL`. Checks 1 and 5 become automatic. The step renders in memory (`template.Render`), so a failed run writes nothing; the fork's `ProcessSchema`, which the `jet` command and `dbgen.go` call, still deletes its output directory first.

---

## 2. HTTP server: oapi-codegen and the nano templates (api)

**Status.** The services run upstream oapi-codegen v2.8.0 with `chi-server` and `strict-server`, and no nano templates. That output is third-party code. The nano strict-server templates are **shipped, unused**, pinned to oapi-codegen v2.8.2. Running oapi-codegen in-process with the templates is **designed** (pipeline step 2). Guide: [HTTP boundary](http-boundary.md), [OpenAPI](generation.md#openapi).

### What the services run today

anchor `codegen.yaml` (7 lines):

```yaml
package: api
generate:
  chi-server: true
  models: true
  strict-server: true
output: ./internal/api/gen.go
```

That writes anchor `internal/api/gen.go` (26,184 lines) from `cmd/http/openapi.yaml` (10,424 lines). echopoint writes `internal/apigen/gen.go` (44,625 lines) from 16,442 spec lines. The services pass their own `RequestErrorHandlerFunc` and `ResponseErrorHandlerFunc`, which write the legacy `errorenvelope` body (anchor `cmd/http/server.go:275-297`).

### What you write with the nano templates

`http/nanohttp/internal/example/oapi-codegen.yaml` (13 lines):

```yaml
package: example
output: api.gen.go
generate:
  models: true
  std-http-server: true
  strict-server: true
additional-imports:
  - package: github.com/nanostack-dev/nanostack-framework/http/nanohttp
  - package: github.com/nanostack-dev/nanostack-framework/http/problem
output-options:
  user-templates:
    strict/strict-http.tmpl: ../../oapitemplates/strict/strict-http.tmpl
    union.tmpl: ../../oapitemplates/union.tmpl
```

The header of `http/nanohttp/oapitemplates/strict/strict-http.tmpl` (236 lines) lists every difference from upstream:

- JSON bodies are read through `nanohttp.Decode` (strict json/v2, then the generated validators).
- Parameters run `validateParams` after binding.
- The default error handlers write RFC 9457 problems.
- `paramProblem` and `NewHTTPHandler` send binding errors to the request error handler.

`http/nanohttp/oapitemplates/union.tmpl` (150 lines) changes only the decoders: `As<Variant>`, `Discriminator` and the property reads decode with strict json/v2 (`nanohttp.Unmarshal`) instead of encoding/json v1. The encoders are upstream's.

### What is generated, per operation

`http/nanohttp/internal/example/api.gen.go` (877 lines). Most of it is upstream output: models, parameter binding, response visitors. The nano parts are the strict wrappers and the tail. A JSON-body operation (lines 675-704):

```go
func (sh *strictHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var request CreateWebhookRequestObject

	var body CreateWebhookJSONRequestBody
	if err := nanohttp.Decode(r, &body); err != nil {   // nano: replaces json.NewDecoder(r.Body).Decode
		sh.options.RequestErrorHandlerFunc(w, r, err)   // already a problem: 400/413/415/422
		return
	}
	request.Body = &body
	// … upstream middleware chain and response visiting, unchanged
}
```

An operation with path parameters runs the parameter check first (`GetWebhook`, lines 706-714):

```go
	// …
	request.Id = id
	if errs := request.validateParams(); len(errs) > 0 {               // nano
		sh.options.RequestErrorHandlerFunc(w, r, problem.InvalidParameters(errs))
		return
	}
```

`nanohttp.Decode` (`http/nanohttp/decode.go:59-74`) checks the media type (415) and size (413), decodes strictly (400 `malformed_json` or `invalid_body` with a JSON Pointer), then calls the generated `Validate` (422 `http.validation_failed`, every violation listed).

### How to review it

- **Review the spec and the codegen config.** Do not review `gen.go` or `api.gen.go`.
- **With the nano templates, check three lines of config:** both `user-templates`, both `additional-imports`, and `std-http-server`. Without the templates the file still compiles, but you get upstream behaviour: no validation, v1 decoding.
- **On an oapi-codegen upgrade,** diff upstream `strict/strict-http.tmpl` and `union.tmpl` against the nano copies. The services resolve v2.8.0 today; the templates are pinned to v2.8.2.
- **Custom error handlers** replace the problem writers. That is how the services keep `errorenvelope`.

### Guardrails

- `http/nanohttp/oapitemplates/oapitemplates_test.go` checks that the embedded templates match the files on disk.
- The template calls methods that only `nano gen validate` writes, so a missing validator (no `validate.gen.go`, or no method for a new body type or operation) is a compile error. A stale validator still compiles; `nano gen validate -check` catches it.
- **Designed:** the api step fails on any other oapi-codegen version with `nano gen api: oapi-codegen v2.9.0 resolved; nano templates are pinned to v2.8.2 (exit 3)` and `fix: go get github.com/oapi-codegen/oapi-codegen/v2@v2.8.2`. CI then checks the oapi server for the first time.

**Do not review by hand:** wrapper bodies, `ServerInterfaceWrapper` binding, the `*Param*Error` types, response visitors and union helpers. The e2e table in `http/nanohttp/internal/example/e2e_test.go` covers each problem type end to end.

---

## 3. Request validators: `nano gen validate`

**Status: shipped, unused.** On `main` (#69), each unenforced feature is its own error with the exact `-allow` fix. Moving `-allow` into the spec as `x-nano-unenforced: "<reason>"` (design P11) is **in progress** in pipeline PR 3. Runtime: `core/validate`. Nothing reads the spec at runtime. Decision: [ADR 0016](adr/0016-generated-request-validation.md).

### What you write

The OpenAPI schema. `http/nanohttp/internal/example/openapi.yaml` (135 lines), lines 109-122. `Auth` is a `oneOf` with a discriminator; `RunWhen` is an enum.

```yaml
    CreateWebhookRequest:
      type: object
      required: [name, url]
      properties:
        name: {type: string, minLength: 1, maxLength: 255}
        description: {type: [string, "null"], maxLength: 1000}
        url: {type: string, format: uri, maxLength: 2048}
        headers:
          type: array
          maxItems: 20
          items: {$ref: '#/components/schemas/Header'}
        auth: {$ref: '#/components/schemas/Auth'}
        run_when: {$ref: '#/components/schemas/RunWhen'}
        retries: {type: integer, format: int32, minimum: 0, maximum: 10}
```

Run oapi-codegen first, then validate on the same package (`http/nanohttp/internal/example/generate.go`). A service writes `//go:generate go tool nano gen validate -spec openapi.yaml -dir .`.

### What is generated

`http/nanohttp/internal/example/validate.gen.go` (279 lines):

```go
// Code generated by nano gen validate from openapi.yaml. DO NOT EDIT.
// … imports, the compiled patterns, and the Auth, BasicAuth and BearerAuth validators

// Validate checks r against #/components/schemas/CreateWebhookRequest.
func (r CreateWebhookRequest) Validate(p validate.Path) validate.Errors {
	var errs validate.Errors
	r.validateInto(p, &errs)
	return errs
}

// validateInto records the violations of r at p in errs.
func (r CreateWebhookRequest) validateInto(p validate.Path, errs *validate.Errors) {
	if !errs.NotNull(p) {
		return
	}
	if f6 := p.Field("name"); errs.Required(f6, true) && errs.NotNull(f6) {
		if n7 := utf8.RuneCountInString(r.Name); n7 < 1 {
			errs.Add(f6, validate.CodeMinLength, "must be at least 1 character long")
		} else if n7 > 255 {
			errs.Add(f6, validate.CodeMaxLength, "must be at most 255 characters long")
		}
	}
	// … description: maxLength 1000, the same shape
	if f10 := p.Field("url"); errs.Required(f10, true) && errs.NotNull(f10) {
		if n11 := utf8.RuneCountInString(r.Url); n11 > 2048 {
			errs.Add(f10, validate.CodeMaxLength, "must be at most 2048 characters long")
		}
		if !validate.URI(r.Url) {
			errs.Add(f10, validate.CodeFormat, "must be an absolute URI")
		}
	}
	// … headers (maxItems, then each item), auth, run_when, retries
}
```

A discriminated `oneOf` decodes the variant strictly and recurses into it (lines 25-47). `additionalProperties: false` becomes `errs.OnlyMembers(p, "name", "value")`. Each operation with parameters gets `validateParams` (lines 243-279):

```go
// validateParams checks the parameters of GET /webhooks/{id}.
func (r GetWebhookRequestObject) validateParams() validate.Errors {
	errs := new(validate.Errors)
	p := validate.Root
	if !pattern1.MatchString(r.Id) {
		errs.Add(p.Field("id"), validate.CodePattern, "must match the pattern ^wh_[a-z0-9]+$")
	}
	return *errs
}
```

`p` carries the raw JSON document, so `Required`, `NotNull` and `Present` check the JSON text, not Go zero values. `{"name":""}` gives `minLength`; `{}` gives `required`.

### How to review it

- **Review the spec diff.** Each `validateInto` starts with a comment naming its schema pointer. Spot-check only the schemas the spec diff touched.
- **Read the gap list.** When the spec uses a feature the validators cannot enforce, the file starts with `// The spec uses these features, which the validators below do not enforce:` and one line per gap (`gen/validate/render.go`). Every `-allow` in a `go:generate` line is a reviewed decision to accept a gap.
- **Check pattern anchors.** `pattern` uses ECMA-262 and is translated to RE2. Check that `^…$` is present.

### Guardrails

Messages as `gen/validate/generate_test.go` and `gen/validate/run_test.go` assert them. The first starts with the `file:line` of the Go declaration, elided here:

```text
StrictServerInterface.Check: no operation POST /check in the spec
  fix: regenerate the oapi-codegen package from this spec with strict-server: true, or pass the -spec it was generated from
openapi.yaml: not enforced: #/components/schemas/Body/properties/note: not is not enforced
  fix: change the schema, or acknowledge the gap with -allow '#/components/schemas/Body/properties/note'
```

- A spec and Go package that disagree is a hard error with no output. Go-side errors carry the `file:line` of the Go declaration. Spec-side errors name the spec file and a schema pointer, not a line; only a YAML syntax error has a line, inside the YAML parser's message.
- An unenforced feature exits 1, but the file is still written and lists the gap. An `-allow` covers that schema and every schema under it (`gen/validate/run.go`).
- `-check` exits 1 when the file is stale.

**Do not review by hand:** the per-field `if fN := p.Field(...)` blocks, the numbered temporaries and the `Validate`/`validateInto` pairs. 14 golden suites in `gen/validate/internal/golden` pin them.

---

## 4. Error catalogues: `nano gen errors`

**Status: shipped, unused.** Runtime: `core/fault`. Decision: [ADR 0022](adr/0022-error-catalogue-from-go-interface.md). Guide: [Errors](generation.md#errors-nano-gen-errors).

### What you write

`gen/errors/testdata/billing/errors.go` (34 lines):

```go
// … the package clause, the imports and the type's doc comment
//nano:errors namespace=billing
type declarations interface {
	// No invoice has the requested id.
	//
	//nano:error code=invoice_not_found status=404 title="Invoice not found"
	InvoiceNotFound(id ids.UUID)

	// The invoice was paid already and cannot be paid again.
	//
	//nano:error code=invoice_already_paid status=409 title="Invoice is already paid" detail="Invoice {id} was paid at {paidAt}."
	InvoiceAlreadyPaid(id ids.UUID, paidAt time.Time)
	// … 2 more methods of the same shape
}
```

- The method signature becomes the constructor.
- `code=` is required. It is the public contract and never follows a rename of the Go method.
- `detail=` placeholders must name parameters.
- The doc comment becomes the Go doc and the catalogue text.

### What is generated

`gen/errors/testdata/billing/errors.gen.go` (268 lines), lines 95-151 with their doc comments trimmed. Every error gets the same block:

```go
// … the header, the imports, the kind list and interface assertions, and the InvoiceNotFound block
var ErrInvoiceAlreadyPaid = fault.NewKind("billing.invoice_already_paid", 409, "Invoice is already paid", "https://errors.nanostack.dev/billing/invoice_already_paid")

type InvoiceAlreadyPaidError struct {
	ID     ids.UUID
	PaidAt time.Time
	cause  error
}

func InvoiceAlreadyPaid(id ids.UUID, paidAt time.Time) *InvoiceAlreadyPaidError {
	return &InvoiceAlreadyPaidError{ID: id, PaidAt: paidAt}
}
// … Wrap, Unwrap, Is, Error, ProblemType, ProblemStatus, ProblemTitle, ProblemCode: the same shape for every error
func (e *InvoiceAlreadyPaidError) ProblemDetail() string {
	return fmt.Sprintf("Invoice %v was paid at %v.", e.ID, e.PaidAt.Format(time.RFC3339))
}
// … 2 more errors of the same shape
```

`gen/errors/testdata/billing/ERRORS.md` (55 lines):

```markdown
<!-- … the header, the title and the intro paragraph -->
| Code | Status | Title | Fields |
|---|---|---|---|
| [`billing.invoice_not_found`](#billinginvoice_not_found) | 404 Not Found | Invoice not found | `id` |
| [`billing.invoice_already_paid`](#billinginvoice_already_paid) | 409 Conflict | Invoice is already paid | `id`, `paidAt` |
<!-- … 2 more rows, then one section per code -->
```

### How to review it

- **Review the interface and the `ERRORS.md` diff.** Skip `errors.gen.go`. A changed `code=` or `status=` is a breaking API change, and it shows as one changed table row.
- **Check that `detail` exposes nothing secret.** `ProblemDetail()` is the only text that reaches a client. `Wrap(cause)` keeps the cause for `errors.Is` and logs; `problem.Write` never sends it.
- **Check that the status fits the meaning:** 409 for a state conflict, 503 for a retryable outage. The generator only checks 400–599.

### Guardrails

Real messages from the fix table in `gen/errors/generrors_test.go`:

```text
errors.go:10: InvoiceGone: missing //nano:error line
  fix: add //nano:error code=invoice_gone status=<4xx|5xx> title="..." to its doc comment
errors.go:10: IPv6Invalid: code is required
  fix: add code=ipv6_invalid …
B: code "a" is already used by A
  fix: set a distinct code=
```

- The generator type-checks the declaration package and its own output.
- Code uniqueness is checked within one namespace only. Keep one namespace per service.
- `-check` exits 1 on stale output.

**Do not review by hand:** the `Wrap`/`Unwrap`/`Is`/`Problem*` boilerplate. The golden test pins it.

---

## 5. Mappers: `nano gen mapper`

**Status: designed** (framework PRs 7-8, ADR 0025 to come). Nothing is generated yet. Design: [Feature 2](design/nano-gen-pipeline.md#feature-2-generated-mappers-nano-gen-mapper), lines 224-355.

### What it replaces

Hand-written mapper structs. anchor `internal/mapper/user_mapper.go` (35 lines) holds the design's "bug 1":

```go
// … the package clause and the imports
type UserMapper struct{}

func NewUserMapper() *UserMapper {
	return &UserMapper{}
}
func (m *UserMapper) ToDomain(
	entity model.Users,
) auth.User {
	return auth.User{
		ID:             entity.ID,
		Email:          entity.Email,
		HashedPassword: entity.HashedPassword,
		ExternalID:     entity.ExternalID,
		CreatedAt:      entity.CreatedAt,
		UpdatedAt:      entity.UpdatedAt,
	}                                    // model.Users.Name (*string) is never read
}

func (m *UserMapper) ToEntity(domain auth.User) model.Users {
	return model.Users{
		ID:             domain.ID,
		Email:          domain.Email,
		ExternalID:     domain.ExternalID,
		HashedPassword: domain.HashedPassword,
		CreatedAt:      domain.CreatedAt,
		UpdatedAt:      domain.UpdatedAt,
	}                                    // Name and Status are never set: users.status is NOT NULL, so every insert writes ''
}
```

- anchor has 147 such functions (2,345 lines); echopoint has 213 (3,965 lines). Only 1 of 97 anchor mapper names and 15 of 207 echopoint names are called from a test (design lines 30-38).
- 35 of the 38 `*Mapper` structs across both services have no fields. They exist only to be wired: anchor `cmd/app/wire.gen.go` builds 23 zero-argument `mapper.NewXxxMapper()` providers, for example `productUserMapper = mapper.NewProductUserMapper()` at line 1235.

### What you write

Calls to `nanomap` inside `func _() { … }`. Go compiles that function but never runs it. The file has no build tag, so `go build`, `go vet` and gopls check every field reference, and a rename updates them. An agent's first draft is one line (design 235-237):

```go
func _() {
	nanomap.Func[model.Users, auth.User]("UserToDomain").Reverse("UserToEntity")
}
```

`nano gen` rejects it (see the errors below). After the fixes (design 293-299):

```go
nanomap.Func[model.Users, auth.User]("UserToDomain",
	nanomap.NilAsZero(auth.User{}.Name),
	nanomap.Unused(model.Users{}.Status, "auth.User has no status yet"),
).Reverse("UserToEntity",
	nanomap.ZeroAsNil(model.Users{}.Name),
	nanomap.Arg(model.Users{}.Status, "status"),
)
```

### What is generated

`mapper.gen.go`, next to the declaration: plain package functions, with no struct and no provider. The design fixes the signatures:

```go
func UserToDomain(src model.Users) auth.User
func UserToEntity(src auth.User, status string) model.Users   // every caller must now pass a status
```

The rules change the signature (design 265). A rule that can fail (a fallible `Via`, `JSON`, string → enum, flattening a pointer) makes the function return `(D, error)`, with errors wrapped as `UserToDomain: Status: …`. A `Via` function that takes a `context.Context` makes the generated function take `ctx` first.

Illustrative only: the design fixes signatures, not bodies, and nothing is generated yet. A body follows from the rules table below:

```go
// Code generated by nano gen mapper. DO NOT EDIT.

func UserToDomain(src model.Users) auth.User {
	return auth.User{
		ID:    src.ID,
		Email: src.Email,
		Name:  functional.FromPtr(src.Name).OrElse(""), // NilAsZero
		// … one line per field of the same shape
	}
}
```

**Enum and pointer.** For anchor `internal/mapper/product_user_mapper.go` (design 306-314):

```go
nanomap.Func[model.ProductUsers, user.ProductUser]("ProductUserToDomain",
	nanomap.NilAsZero(user.ProductUser{}.Name),
).Reverse("ProductUserToEntity",
	nanomap.ZeroAsNil(model.ProductUsers{}.Name), // today: OptionOf(name, name != "").ToPtr()
)
```

This generates `ProductUserToDomain(...) (user.ProductUser, error)`, because string → enum fails on an unknown value by default. Today's mapper casts without a check: `Status: user.ProductUserStatus(entity.Status)` (line 27). Keeping that needs `nanomap.Cast(field, "<reason>")` (open question 2).

**Handler input.** anchor has 141 inline literals like this one, `internal/api/organization_api.go:111-118`:

```go
input := organization.CreateOrganizationInput{
	TenantID:          tenantID,
	ProductID:         request.ProductId,
	Name:              request.Body.Name,
	Description:       request.Body.Description,
	Metadata:          mapMetadataToInput(request.Body.Metadata),
	LicenseTemplateID: licenseTemplateID,
}
```

The declaration that replaces it, in `internal/api/mappers.go` (design 319-329):

```go
nanomap.Func[api.ProductOrganizationRequest, organization.CreateOrganizationInput]("CreateOrganizationInput",
	nanomap.Arg(organization.CreateOrganizationInput{}.TenantID, "tenantID"),
	nanomap.Arg(organization.CreateOrganizationInput{}.ProductID, "productID"),
	nanomap.Arg(organization.CreateOrganizationInput{}.LicenseTemplateID, "licenseTemplateID"),
	nanomap.Via(organization.CreateOrganizationInput{}.Metadata, mapMetadataToInput),
	nanomap.Unused(api.ProductOrganizationRequest{}.FoundingMember, "CreateWithMember path"),
	nanomap.Unused(api.ProductOrganizationRequest{}.License, "resolved to licenseTemplateID by the handler"),
)
```

The call site becomes `CreateOrganizationInput(*request.Body, tenantID, request.ProductId, licenseTemplateID)`.

Domain↔entity mappers go in `internal/mapper` (anchor) or the feature package (echopoint). api↔domain mappers go in `internal/api`, because a separate package would create an import cycle.

### Rules a reviewer should know (design 244-263, trimmed)

| Case | Rule |
| --- | --- |
| Field names | Exact match, then initialisms folded (`ID`/`Id`, `URL`). Two candidates for one field is an error. |
| `*T`→`T`, `Option[T]`→`T` | An error unless `NilAsZero` or `Default` is declared. |
| string → enum | Fails on an unknown value by default. `Cast(field, why)` opts out. |
| Different names | `From(target, source)` maps a source field with another name. `Reverse` swaps it. |
| Nested struct | Needs a declared `Func` for that pair, or a hand-written function registered once with `Use(fn)`. Nothing is derived implicitly. |
| A target field with no source, or a source field never used | An error on both sides. Waived only by `Skip(field, why)` or `Unused(field, why)`. |
| `Reverse` | Inverts renames, casts, `Flatten` and enums. It never inverts `NilAsZero`, `Default`, `Arg`, `Via`, `After`, `Skip` or `Unused`; each needs its own rule. |
| JSON decode failure | One policy: return the error, with the field path. |

### Errors an agent sees (design 282-287 and 345-354)

```text
internal/mapper/mappers.go:6: UserToDomain (model.Users → auth.User): model.Users.Name is *string, auth.User.Name is string
  fix: nanomap.NilAsZero(auth.User{}.Name), or nanomap.Default(auth.User{}.Name, "<value>")
internal/mapper/mappers.go:6: UserToDomain: source field model.Users.Status is not used
  fix: map it, or nanomap.Unused(model.Users{}.Status, "<why>")
internal/mapper/mappers.go:6: UserToEntity (auth.User → model.Users): target field model.Users.Status has no source
  fix: nanomap.Arg(model.Users{}.Status, "status"), or nanomap.Skip(model.Users{}.Status, "<why>")
internal/mapper/mappers.go:21: ProductUserToEntity: NilAsZero(user.ProductUser.Name) cannot be inverted
  fix: nanomap.ZeroAsNil(model.ProductUsers{}.Name) in Reverse, or declare ProductUserToEntity as its own Func
internal/mapper/mappers.go:30: OrganizationToDomain: Unused(model.Organizations.DeletedAt) needs a reason
  fix: nanomap.Unused(model.Organizations{}.DeletedAt, "soft delete is read by the repository")
// … 3 more errors of the same shape (design 345-354)
```

On today's code these rules report bug 1 (anchor `Users.Name` and `Users.Status`), bug 2 (anchor `ExpiresAt` has no source) and bug 3 (echopoint `ScheduleRunId` and `TimeoutSeconds` are never set).

### How to review it

- **Review the declarations only.** Check that every `Skip`, `Unused`, `Cast`, `NilAsZero` and `Default` has a true reason, that each `Arg` order matches what callers expect, and that each `Reverse` states its own rules. The waiver reasons are the decisions in this code.
- **Review call sites whose signature changed.** A rule that becomes fallible, takes a context or adds an `Arg` breaks every caller. That is where behaviour changes.
- **Do not review `mapper.gen.go`.** The generator type-checks its own output, and `-check` fails on drift.
- **Some mappers stay hand-written,** for example echopoint `executions/db_mapper.go` (it takes a logger; open question 3). A generated mapper can call them through `Via`.

When mappers are generated, the 23 mapper provider blocks leave anchor's `wire.gen.go`.

---

## 6. Handlers by tag: `handlers.gen.go`

**Status: designed** (framework PR 5, ADR 0024 to come). Written by the api step, in the oapi package. Design: [Feature 3](design/nano-gen-pipeline.md#feature-3-the-api-split-by-openapi-tag), lines 357-455.

### Today

One flat interface carries all 133 operations. anchor `internal/api/gen.go:21651`:

```go
// StrictServerInterface represents all server handlers.
type StrictServerInterface interface {
	// Login User login
	// (POST /v1/auth/login)
	Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error)
	// … 132 more operations of the same shape
}
```

One struct serves it. Its constructor, anchor `internal/api/impl.go:60`, takes 31 parameters, two of them unused (`TenantService`, `Queue`). Every handler method hangs off it, for example anchor `internal/api/email_api.go:399`:

```go
func (s *AnchorAPI) SendEmail(
	ctx context.Context, request SendEmailRequestObject,
) (SendEmailResponseObject, error) {
	// …
```

### What you write

No new declaration. The source of truth is the tag on each operation in `openapi.yaml`; each operation must have exactly one. The hand-written handler's constructor returns its tag interface (design 399-403):

```go
type emailHandler struct{ emails emailsvc.EmailService }

func NewEmailHandler(emails emailsvc.EmailService) EmailServer { return &emailHandler{emails} }
```

The manifest lists `api.NewEmailHandler` and `api.NewHandlers` in `nano.Providers`, with no `nano.Bind`.

### What is generated (design 363-391; a sketch, nothing generated yet)

```go
// Code generated by nano gen api. DO NOT EDIT.

// EmailServer serves the operations tagged Email.
type EmailServer interface {
	// (POST /v1/products/{product_id}/email/sends)
	SendEmail(ctx context.Context, request SendEmailRequestObject) (SendEmailResponseObject, error)
	// … the other 14 Email operations
}
// … one interface per tag (21 in anchor)

type Handlers struct {
	email     EmailServer        // unexported: set once, never nil
	licensing LicensingServer
	// …
}

// NewHandlers refuses a nil handler and names its tag.
func NewHandlers(email EmailServer, licensing LicensingServer /* , ... */) (*Handlers, error)

func (h *Handlers) SendEmail(ctx context.Context, r SendEmailRequestObject) (SendEmailResponseObject, error) {
	return h.email.SendEmail(ctx, r)
}
// … one delegator per operation (133 in anchor, about 190 in echopoint)

var _ StrictServerInterface = (*Handlers)(nil) // the flat upstream interface is untouched
```

Tag names become Go names through `codegen.ToCamelCase`: `"Status pages"` becomes `StatusPagesServer`.

### Rollout in anchor (design 419-424)

1. Fix the spec: it uses 21 tags but declares 19. Six are used but undeclared; four are declared but unused.
2. Compile at once with 21 one-line shims, for example `func NewEmailServer(a *AnchorAPI) EmailServer { return a }`.
3. Replace the shims one file at a time. When the last one is gone, delete `NewAPI` and `AnchorAPI`.

echopoint must retag first: 8 of its tags span several handlers.

### Errors an agent sees (design 438-451; `<line>` is a placeholder)

```text
cmd/http/openapi.yaml:<line>: operation ListEmailSends has 2 tags (Email, Product); a tag is a handler boundary
  fix: keep one tag
cmd/http/openapi.yaml:<line>: tag Email is used by 15 operations but not declared
  fix: add "- name: Email" under the top-level tags
internal/api/email_api.go:14: *emailHandler does not implement EmailServer (missing method SendEmail)
  fix: func (h *emailHandler) SendEmail(ctx context.Context, request SendEmailRequestObject) (SendEmailResponseObject, error)
nano gen wire: NewHandlers: no provider for api.EmailServer
  fix: add a provider that returns api.EmailServer to nano.Providers in cmd/app/manifest.go
// … 3 more errors of the same shape
```

### How to review it

- **Review the tag on each operation.** A tag is now a code boundary.
- **Review each handler's constructor parameters.** A handler gets only its own services, so a dependency it does not need is easy to see.
- **Do not review `handlers.gen.go`.** Each delegator is one line. `var _ StrictServerInterface = (*Handlers)(nil)` proves at compile time that every operation is covered.
- **Trade-off:** the constructor returns an interface. Wire treats an interface-typed provider's lifecycle as dynamic (`gen/wire/checks.go`). Handlers have no `Start` or `Stop`, so that costs nothing.

---

## 7. Configuration: `nano gen config`

**Status: shipped.** On `main` (#69), the generator loads the package with `-tags=nanogen`. That removes the config/wire deadlock (a renamed `Config` field with a stale `wire.gen.go` now generates), and `Config` must sit in a file without a build tag. Guide: [Configuration](generation.md#configuration-nano-gen-config).

### What you write

One root `Config` of named sections. Each section is a struct owned by its module. A field's doc comment becomes its description everywhere.

| Tag | Meaning |
| --- | --- |
| `config:"name[,optional]"` | A lower snake case path segment. A nested struct adds a segment (`core.auth.x`). `config:"-"` skips the field. |
| `env:"NAME"` | Overrides the env var, which is otherwise derived from the path (`CORE_AUTH_X`). |
| `default:"v"` | Parsed and validated at generation time. A setting with no default is required unless it is `optional`. A secret cannot have a default. |
| `defaultenv:"NAME"` | Reads `NAME` when the setting's own env and the config files leave it unset, before `default`. See [One variable, two settings](#one-variable-two-settings). A secret cannot have one. |
| `validate:"min=,max=,oneof=,scheme="` | Becomes typed `Between`, `Min`, `OneOf` or `Scheme` calls. On a slice, the rule applies to each element. |

A field is secret by type: `secret.Secret`. The grammar is in `gen/config/run.go`.

echopoint `cmd/app/config.go` (80 lines), lines 33-62, 14 sections:

```go
type Config struct {
	// App bounds the start and the stop of the process.
	App nano.AppConfig `config:"app"`
	// Log is the process logger.
	Log logging.Settings `config:"log"`
	// Tracing is the OpenTelemetry export (OTEL_* variables).
	Tracing nanotrace.Config `config:"tracing"`
	// Postgres is the database connection pool.
	Postgres nanopg.Config `config:"postgres"`
	// … 9 more sections of the same shape, from migrations to cloud_runner
	// CloudWorker is the queue the Cloud worker consumes.
	CloudWorker executions.CloudflareWorkerConfig `config:"cloud_worker"`
}
```

### What is generated

| Service | Source | `config.gen.go` | `CONFIG.md` | `.env.example` |
| --- | --- | --- | --- | --- |
| echopoint | `cmd/app/config.go` (80 lines) + module sections | 526 lines | 79 lines | 250 lines |
| anchor | `cmd/app/app.go` (46) + `internal/service/config/config.go` (65) + module sections | 421 | 66 | 198 |

`config.gen.go` holds `Bind(ctx, *nanoconf.Sources) (Config, error)`, one block per setting with no reflection, plus `LogValue()`, `String()` and `GoString()`. echopoint `cmd/app/config.gen.go`:

```go
// Code generated by nano gen config from config.go. DO NOT EDIT.
// … the package clause and the imports

// Bind reads every Config setting from src, applies defaults, parses and
// validates each value, and returns all issues together as a
// *nanoconf.Error. Precedence: environment variable, then the secret store
// (secret settings only), then config files, then the default.
func Bind(ctx context.Context, src *nanoconf.Sources) (Config, error) {
	var c Config
	b := nanoconf.NewBinder(ctx, src)

	// app.start_timeout: StartTimeout bounds the whole start phase: every Starter together.
	{
		k := nanoconf.Key{Path: "app.start_timeout", Env: "APP_START_TIMEOUT", Default: "15s"}
		c.App.StartTimeout, _ = nanoconf.Scalar(b, k, nanoconf.ParseDuration)
	}

	// … 5 more blocks, app.stop_timeout to log.format

	// log.wire: Wire is legacy (the zerolog line shape) or slog.
	{
		k := nanoconf.Key{Path: "log.wire", Env: "LOG_WIRE", Default: "legacy"}
		if v, ok := nanoconf.Scalar(b, k, nanoconf.ParseString[string]); ok {
			c.Log.Wire = v
			nanoconf.OneOf(b, k, v, "legacy", "slog")
		}
	}
	// … 54 more blocks of the same shape, one per setting, in declaration order
	if err := b.Err(); err != nil {
		return Config{}, err
	}
	return c, nil
}
```

A block has one of three shapes: plain (`c.X, _ = Scalar(...)`), validated (`if v, ok := Scalar(...); ok { c.X = v; Min(...) }`) or list (`List(...)`, with the rule applied per element through `k.Elem(i)`).

### One setting through all three outputs

anchor `internal/service/config/config.go:29-30`:

```go
	// RefreshTokenLifetime is the lifetime of a refresh token, in seconds.
	RefreshTokenLifetime int64 `config:"refresh_token_lifetime" env:"REFRESH_TOKEN_LIFETIME" default:"604800" validate:"min=1"`
```

anchor `cmd/app/config.gen.go:278-285` and `:400`:

```go
	// core.auth.refresh_token_lifetime: RefreshTokenLifetime is the lifetime of a refresh token, in seconds.
	{
		k := nanoconf.Key{Path: "core.auth.refresh_token_lifetime", Env: "REFRESH_TOKEN_LIFETIME", Default: "604800"}
		if v, ok := nanoconf.Scalar(b, k, nanoconf.ParseInt[int64]); ok {
			c.Core.Auth.RefreshTokenLifetime = v
			nanoconf.Min(b, k, v, 1)
		}
	}
	// … in LogValue:
				slog.Int64("refresh_token_lifetime", c.Core.Auth.RefreshTokenLifetime),
```

anchor `cmd/app/CONFIG.md:59` and `.env.example:168-170`:

```text
| `core.auth.refresh_token_lifetime` | `REFRESH_TOKEN_LIFETIME` | int64 | `604800` | no | no | `min=1` | RefreshTokenLifetime is the lifetime of a refresh token, in seconds. |

# core.auth.refresh_token_lifetime (int64; min=1)
# RefreshTokenLifetime is the lifetime of a refresh token, in seconds.
# REFRESH_TOKEN_LIFETIME=604800
```

The `env:` tag keeps the name the deployed stacks set. Without it, the name would be `CORE_AUTH_REFRESH_TOKEN_LIFETIME`.

### One variable, two settings

Two settings cannot share an `env`. A setting that should follow another one's variable reads it as its default with `defaultenv:"NAME"`: its own variable first, then the config files, then `NAME`, then the literal `default`.

```go
	Environment string `config:"environment" defaultenv:"ENVIRONMENT" default:"production"`
```

```go
		k := nanoconf.Key{Path: "core.environment", Env: "CORE_ENVIRONMENT", DefaultEnv: "ENVIRONMENT", Default: "production"}
```

```text
| `core.environment` | `CORE_ENVIRONMENT` | string | env `ENVIRONMENT`, else `production` | no | no |  | … |
```

A value read from `NAME` that fails a check names its origin: `got "staging" from env ENVIRONMENT (the default)`.

### Secrets

- **No default.** The generator refuses `default:` and `defaultenv:` on a `secret.Secret` field, and a `defaultenv:` that names the env of a secret setting.
- **Never from a config file.** If a YAML layer holds the key, Bind fails, even when env would win.
- **Never printed.** `LogValue` calls `.String()`, which returns `[REDACTED]`. `secret.Secret` also redacts `%v`, `%#v`, `slog` and JSON/YAML/text marshalling (`core/secret/secret.go`). URLs are logged through `secret.RedactURL`.

```go
	// anchor.product_api_key: ProductAPIKey authenticates echopoint as its product.
	{
		k := nanoconf.Key{Path: "anchor.product_api_key", Env: "ANCHOR_PRODUCT_API_KEY", Secret: true}
		c.Anchor.ProductAPIKey, _ = nanoconf.Scalar(b, k, nanoconf.ParseSecret)
	}
```

### What a diff looks like when a field is added

Adding one field to anchor's `AuthConfig`:

```diff
+	// ResetTokenLifetime is how long a password reset link stays valid.
+	ResetTokenLifetime time.Duration `config:"reset_token_lifetime" default:"1h" validate:"min=5m,max=24h"`
```

gives a diff of 4 files and 18 insertions, the 2 source lines included (blank lines are trimmed below):

```diff
--- a/cmd/app/config.gen.go
+	"time"
+	// core.auth.reset_token_lifetime: ResetTokenLifetime is how long a password reset link stays valid.
+	{
+		k := nanoconf.Key{Path: "core.auth.reset_token_lifetime", Env: "CORE_AUTH_RESET_TOKEN_LIFETIME", Default: "1h"}
+		if v, ok := nanoconf.Scalar(b, k, nanoconf.ParseDuration); ok {
+			c.Core.Auth.ResetTokenLifetime = v
+			nanoconf.Between(b, k, v, 5*time.Minute, 24*time.Hour)
+		}
+	}
+				slog.Duration("reset_token_lifetime", c.Core.Auth.ResetTokenLifetime),
--- a/cmd/app/CONFIG.md
+| `core.auth.reset_token_lifetime` | `CORE_AUTH_RESET_TOKEN_LIFETIME` | duration | `1h` | no | no | `min=5m,max=24h` | ResetTokenLifetime is how long a password reset link stays valid. |
--- a/.env.example
+# core.auth.reset_token_lifetime (duration; min=5m,max=24h)
+# ResetTokenLifetime is how long a password reset link stays valid.
+# CORE_AUTH_RESET_TOKEN_LIFETIME=1h
```

The one thing to catch in this diff: the new field got the path-derived `CORE_AUTH_RESET_TOKEN_LIFETIME`, while every sibling has a flat `env:"…"` name. The generator cannot know which name a deployment expects. Read the **Env column of the `CONFIG.md` diff**.

### Guardrails

At generation time, every problem is reported together. Four bad fields added to anchor's `AuthConfig` give these messages (fix texts from `gen/config/load.go` and `gen/config/validate.go`):

```text
internal/service/config/config.go:36: ResetTokenLifetime: default "30s": is below min=5m
  fix: change the default so that it passes the validate rules, or change the rules
internal/service/config/config.go:38: ResetMailFrom: config name "resetMailFrom" must be lower snake case
  fix: write the name in lower snake case, for example config:"due_days"
internal/service/config/config.go:40: ResetPepper: secrets cannot have a default, it would put the secret in source code
  fix: remove the default, and set the secret in the environment
internal/service/config/config.go:41: ResetDebug: the field has no config tag
  fix: add a config:"name" tag, or config:"-" to skip the field
```

- The generator also refuses a duplicate env var or path, `default:""`, `,optional` with a default, `validate:"required"`, unknown rules, `min` greater than `max`, a rule on the wrong type, unsupported types and a lower-case `env`.
- A broken `config.gen.go` never blocks regeneration. The loader replaces it with stubs, so only hand-written code has to compile.
- **Freshness:** echopoint `cmd/app/generated_test.go` (`TestConfigBinderIsCurrent`) and anchor `cmd/app/app_test.go` (`TestGeneratedFilesAreFresh`) call `genconfig.Run(Options{…, Check: true})`.
- **Committed YAML:** echopoint `TestCommittedConfigBinds` and anchor `TestCommittedFileLayersBind` bind every committed `application.yaml`. An unknown key or a secret in a file fails the test.
- **At runtime,** Bind lists every issue at once. One `*nanoconf.Error` holds these three issues (`core/config/errors.go`); its `Error()` prints them on one line, after `config: 3 issues:` and separated by `; `:

```text
server.port (env SERVER_PORT): must be an integer; got "http" from env SERVER_PORT
core.auth.refresh_token_lifetime (env REFRESH_TOKEN_LIFETIME): must be at least 1; got "0" from env REFRESH_TOKEN_LIFETIME
server.prot: unknown setting; fix the spelling or remove it; from file application.yaml
```

### How to review it

1. **The struct tags in the source diff.** Is the path right? Is an `env:` tag needed to keep a deployed name? Is the default sensible? Optional or required? Is the range right? Is every credential typed `secret.Secret`? A credential typed `string` is the one leak the generator cannot see.
2. **The `CONFIG.md` diff.** Read the Env, Default, Required, Secret and Validation columns. A new required setting means every deployment and every test env map must set it.
3. **Defaults of `encoding.TextUnmarshaler` types** such as `slog.Level`. These are the only defaults not checked at generation time. Neither service has one today.
4. **`go test ./cmd/app` is green.**

**Do not review by hand:** the body of `Bind`, the slog group nesting, `.String()` on secrets, `RedactURL` on URLs, and the `.env.example` layout. They follow from the tags.

**Known cosmetic quirks:** a required secret reads `(required; secret; secret)` in `.env.example` (echopoint `.env.example:200`). The comment above each Bind block keeps only the first line of a doc comment, so it can stop mid-sentence; `CONFIG.md` has the full text.

---

## 8. Wiring: `nano gen wire`

**Status: shipped.** On `main` (#69), errors print their fix on an indented line. Becoming step 7 of `go tool nano gen` is **designed**. Guide: [Wiring](generation.md#wiring-nano-gen-wire). Decision: [ADR 0013](adr/0013-generated-wiring-replaces-fx.md).

| Service | You write: `cmd/app/manifest.go` | Generated: `cmd/app/wire.gen.go` | `need*` lines | `app.Manage` lines |
| --- | --- | --- | --- | --- |
| anchor | 56 lines | 2,218 lines | 162 | 96: 13 concrete lifecycle, 77 `interface: lifecycle by dynamic type`, 6 override-only |
| echopoint | 98 lines | 2,758 lines | 201 | 119: 25 concrete, 93 dynamic, 1 override-only |

### What you write: two layers

**The manifest.** A list of modules behind the `nanogen` build tag, so it never links into the binary. anchor `cmd/app/manifest.go` (56 lines):

```go
//go:build nanogen

package app
// … 26 lines of imports

// App is anchor's manifest: nano gen wire reads it and writes wire.gen.go.
var App = nano.Manifest("anchor",
	applog.Module,
	nanotrace.Module,
	nanopg.Module,
	nanomigrate.Module,
	nanocache.Module,
	mapper.Module,
	repository.Module,
	// … 14 more modules of the same shape
	httpserver.Module,
)
```

**Each package's `module.go`.** This is where `Providers`, `Bind` and `Collect` live. A `wire.gen.go` diff is explained by a diff here. anchor `internal/authz/module.go:31-53`: (the `//nolint:gochecknoglobals` comment on each `nano.Module(` line is trimmed in these excerpts)

```go
var Module = nano.Module("authz",
	nano.Providers(
		engine.New,
		store.New,
		// … 4 more constructors
		NewSyncer,
		NewChecker,
	),
	nano.Bind[fgasync.Engines, engine.Provider](),
	nano.Bind[fgasync.Stores, store.Stores](),
	nano.Bind[fgasync.ModelSource, source.Catalog](),
	nano.Collect[fgasync.Source](
		source.NewAssignments,
		source.NewRoleGrants,
		// … 3 more members
	),
	nano.Collect[events.TxObserver](NewMarker, source.NewModelSizeGuard),
)
```

A collection can span modules. A library declares it empty, and a feature adds a member:

```go
// echopoint internal/pgkit/pgkit.go:33-40
var Module = nano.Module("pgkit",
	nano.Providers(
		NewQueue, NewQueueSchema, NewRetention,
		NewWorkflowClient, NewWorkflowSchema, NewWorkflowWorker,
		NewDashboard,
	),
	nano.Collect[workflow.Definition](),
)

// echopoint internal/feature/openapisyncs/module.go:9-20
var Module = nano.Module("openapisyncs",
	nano.Providers(
		NewOpenAPISyncConfigMapper,
		// … 5 more constructors
		NewOpenAPISyncCron,
	),
	nano.Collect[workflow.Definition](SyncWorkflowDefinition),
)
```

The DSL is `core/nano/manifest.go` (96 lines): `Manifest`, `Module`, `Providers`, `Bind[I, T]`, `Collect[T]` and `Invoke`. They are plain data; the generator reads their arguments from source.

### The shape of wire.gen.go

**Header and entry points.** echopoint `cmd/app/wire.gen.go:1-3` and `:482-488`:

```go
// Code generated by nano gen wire from cmd/app/manifest.go. DO NOT EDIT.

//go:build !nanogen
// …
// Build constructs the echopoint app. It calls each needed provider once, in
// dependency order, and registers every component's lifecycle with the App.
// If a provider fails, Build stops what it already constructed.
func Build(ctx context.Context, cfg Config, ov Overrides) (*nano.App, error) {
	app, _, err := BuildComponents(ctx, cfg, ov)
	return app, err
}
```

Production calls `Build(ctx, cfg, Overrides{})`: echopoint `cmd/app/app.go:22`, anchor `cmd/app/run.go:61`.

**`Overrides`: one nilable field per provided value** (echopoint `:71-273`). A set field skips that provider and every provider only it needed. `Components` has the same fields with values; it replaces `fx.Populate` (anchor `cmd/it/shared/test_setup.go:283-299`).

```go
type Overrides struct {
	Settings                             *log.Settings
	LoggingLogger                        *logging.Logger
	SlogLogger                           *slog.Logger
	// … 198 more fields of the same shape
}
```

Field names follow the type name. When two types share a name, both are qualified by their package name: `*nanopg.DB` becomes `NanopgDB` and `*sql.DB` becomes `SQLDB`. When the packages share a name too, the import path qualifies them: `billing/repository.Repository` and `invoice/repository.Repository` become `BillingRepositoryRepository` and `InvoiceRepositoryRepository`. Reordering providers renames no field; a new provider renames only the fields whose type name it shares ([the rule](generation.md#overrides-and-components)). Variables follow the fields (`nanopgDB`, `needNanopgDB`). The quotes below come from files generated before that: a numbered variable in them (`needServer2`, `db2`) is named after its field in current output (`needNanoserverServer`).

**The demand chain: one `need*` line per value, computed before anything is built** (echopoint `:499-506`):

```go
	// A value is built only when something that runs needs it and no
	// override replaces the value that needs it.
	needServer2 := true
	needHandler23 := needServer2 && ov.NanoserverServer == nil
	needAuthMiddleware := needHandler23 && ov.HTTPHandler == nil
	needIdentity := needAuthMiddleware && ov.AuthMiddleware == nil
	needResolver2 := needAuthMiddleware && ov.AuthMiddleware == nil
	needDocument := (needResolver2 && ov.ApisecResolver == nil) || (needAuthMiddleware && ov.AuthMiddleware == nil)
	// … 195 more lines of the same shape
```

Read `needX := (needA && ov.A == nil) || (needB && ov.B == nil)` as: each `||` term is one consumer of X. X is built when a consumer that is itself built takes it and is not overridden. A `:= true` line is a root: a Starter or Runner that nothing takes. A root always runs. The emitter is `demand` in `gen/wire/emit.go`.

### Four block shapes

**1. A Runner, with error wrapping.** anchor `cmd/app/wire.gen.go:1046-1057`, from `NewSyncer` in the authz module:

```go
	var syncer *fgasync.Syncer
	if needSyncer {
		if ov.Syncer != nil {
			syncer = ov.Syncer
		} else {
			syncer, err = authz.NewSyncer(engines, stores2, repository9, locker, modelSource, sources, logger)
			if err != nil {
				return nil, nil, fmt.Errorf("nano: provide *fgasync.Syncer: authz.NewSyncer: %w", err)
			}
		}
		app.Manage("*fgasync.Syncer", syncer) // Runner
	}
```

- The comment after `app.Manage` names what the generator found on the static type: `Starter`, `Stopper`, `Runner` or `HealthChecker`.
- An override is managed too: `Manage` sits outside the `if/else`.
- The error is always `nano: provide <type>: <pkg.Func>: %w`, with no file position, so moving a provider does not churn the file.

**2. Bind.** anchor `:686-704`, from `nano.Bind[fgasync.Engines, engine.Provider]()`:

```go
	var provider3 engine.Provider
	if needProvider3 {
		if ov.EngineProvider != nil {
			provider3 = ov.EngineProvider
		} else {
			provider3 = engine.New(cfg.Postgres, db2, logger)
		}
		app.Manage("engine.Provider", provider3) // interface: lifecycle by dynamic type
	}

	var engines fgasync.Engines
	if needEngines {
		if ov.Engines != nil {
			engines = ov.Engines
			app.Manage("fgasync.Engines", engines)
		} else {
			engines = provider3
		}
	}
```

`Manage` appears only in the override branch of a Bind. The implementation already registered its lifecycle. This is not a missing `Manage`.

**3. Collect.** echopoint `:1108-1118`, from the pgkit and openapisyncs modules above:

```go
	var definitions []workflow.Definition
	if needDefinitions {
		if ov.Definitions != nil {
			definitions = ov.Definitions
			for _, elem := range definitions {
				app.Manage("workflow.Definition", elem)
			}
		} else {
			definitions = []workflow.Definition{syncWorkflowDefinition}
		}
	}
```

The slice lists members in declaration order across the manifest. A collection is overridden as a whole.

**4. The Invoke tail.** `nanomigrate.Module` holds `nano.Invoke(Run)` (`pg/nanomigrate/nanomigrate.go`). It becomes the last statement before the return (anchor `:2214-2216`):

```go
	if err = nanomigrate.Run(ctx, migrator); err != nil {
		return nil, nil, fmt.Errorf("nano: invoke nanomigrate.Run: %w", err)
	}
```

**Override reach.** echopoint `cmd/it/shared/test_setup.go:407-408` sets one field:

```go
	built, components, err := app.BuildComponents(ctx, cfg, app.Overrides{AuthProvider: NewTestBearerAuthenticator()})
```

`auth.NewClerkProvider` is a leaf, so this skips only Clerk. On the worker chain (`:660-662`), setting `ov.WorkflowWorker` skips the `Definitions` collection and its member, but the service behind it stays built, because other terms in its `need*` line keep it alive:

```go
	needWorkflowWorker := true
	needDefinitions := needWorkflowWorker && ov.WorkflowWorker == nil
	needSyncWorkflowDefinition := needDefinitions && ov.Definitions == nil
```

### Lifecycle and order

- **Start order** is `app.Manage` order: dependency order, following manifest order wherever dependencies allow (`gen/wire/graph.go`). Starters run once, under `StartTimeout`. Runners run after every Starter.
- **Stop:** Runners are cancelled and drained. Then Stoppers run one at a time in reverse order.
- **A build failure** returns through a `defer` that calls `app.Stop(ctx)` on everything managed so far.
- **A `func(…) (T, func(), error)` provider** generates `app.OnCleanup`. Neither service uses this form today.

The concrete lifecycle baseline to diff against:

| Service | Starters | Stoppers | Runners |
| --- | --- | --- | --- |
| anchor (13) | `*nanotrace.Installer`, `*resourcetype.SchemaCheck`, `*service.ProductPermissionStartupSync` | `*nanotrace.Provider`, `*nanopg.DB` | `*fgasync.Syncer`, `*service.APIKeyEventWorker`, `*service.IntegrationEventWorker` (also Starter), `*service.QueueRetention`, `*events.Worker`, `*service.LicenseTemplateSyncWorker`, `*nanoserver.Server`, `*httpserver.QueueDashboard` |
| echopoint (25) | `*nanotrace.Installer`, `*pgkit.QueueSchema`, `*pgkit.WorkflowSchema` | `*nanotrace.Provider`, `*nanopg.DB` | 20: pgkit Retention, WorkflowWorker and Dashboard; `*nanopprof.Server`; 11 crons, workers and observers; `*executions.CloudJobPool`; two `SyncStarter` types (Runners despite the name); `*webhooks.RunWebhookSweep`; `*nanoserver.Server` |

### Guardrails

- **Freshness.** anchor `cmd/app/app_test.go` (`TestGeneratedFilesAreFresh`) and echopoint `cmd/app/generated_test.go` (`TestWiringIsCurrent`) call `wire.Run(wire.Options{Dir: ".", Out: ".", Check: true})`.
- **Generation errors** carry `file:line` and a fix, not a panic at boot. From `gen/wire/testdata/*/errors.golden`:

```text
nano.go:9: dependency cycle: *cycle.A (cycle.NewA, nano.go:9) -> *cycle.B (cycle.NewB, nano.go:10) -> *cycle.C (cycle.NewC, nano.go:11) -> *cycle.A
  fix: break it with an interface, or by moving the shared part into its own provider
nano.go:10: unused provider unused.NewMetrics: no Runner, Starter or nano.Invoke depends on *unused.Metrics
  fix: remove it from nano.Providers
```

Other errors in the same style: a duplicate provider for a type, a provider that duplicates a `Config` field, and a value-returning provider whose `Start` has a pointer receiver.

### What to check in a wire.gen.go diff

1. **A new `app.Manage(...) // Starter|Runner|Stopper` line** means new lifecycle: something now starts, runs a goroutine or flushes at shutdown. Compare it with the baseline, then read that type's `Start`, `Run` and `Stop`.
2. **A new `needX := true` line** is a new root. It always runs, in every integration test too.
3. **`// interface: lifecycle by dynamic type` lines are opaque.** That is 77 of 96 in anchor and 93 of 119 in echopoint. If the concrete value behind an interface has `Start` or `Run`, the diff does not say so. Flag any new interface-returning provider that wraps a worker or a client with a lifecycle.
4. **Order.** Reordering modules can reorder Start and Stop. Check that a moved Stopper still stops after the components that use it.
5. **Override reach.** A removed `||` term is a consumer that no longer takes the value. If the only remaining term is a consumer that a test overrides, the value is never built in that test.
6. **A new `fmt.Errorf("nano: provide …` site** is a new fallible constructor. Check that the error is real.
7. **The Invoke tail.** A new statement after the last provider runs at build time, before Start.

**Do not review by hand:** the `need*` chains, the `Overrides` and `Components` field lists, the `return &Components{…}` literal, import aliases such as `auth2`, and ordinary provider blocks with no `Manage` line.

---

## 9. The pipeline that runs them: `go tool nano gen`

**Status.** The nested `gen` module (#68) and the error contract (#69) are **on main**. The pipeline core (`nanogen/project.go`, fixed order, `-check` overlays, orphans, `-json`, `-list`) is **in progress** (PR 3, ADR 0023 to come): it covers config, errors, validate and wire. The db, api, tag split, mapper and lint steps are **designed** (PRs 4-9). The excerpts below are from the design; the code may still change them. Design: [Feature 1](design/nano-gen-pipeline.md#feature-1-the-nano-gen-pipeline).

```text
go tool nano gen                     # run every step in order, write what changed
go tool nano gen -check              # write nothing; exit 1 listing every stale, missing or orphaned file
go tool nano gen -check -fresh       # CI: also ignore the DB cache
go tool nano gen -only api,mapper    # these steps, still in pipeline order (-skip db is the inverse)
go tool nano gen -list               # print the plan: step, package, inputs, outputs
# … -json, -diff and single-generator runs (design 85-94)
```

**What you write** (design 101-119):

```go
// … the package doc comment, the package clause and the import
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

Everything else is discovered with `go/parser`: a `//nano:errors` interface, any file that calls `nanomap.Func`, and a `//go:build nanogen` file that calls `nano.Manifest`.

| # | Step | Needs |
| --- | --- | --- |
| 1 | db | Docker or `NANO_GEN_DATABASE_URL` |
| 2 | api (oapi-codegen v2.8.2 in-process, then `handlers.gen.go`) | the spec |
| 3 | validate | api |
| 4 | errors | none |
| 5 | repo | db |
| 6 | config | validate, errors, repo |
| 7 | wire | config |

The mapper step (part 2) joins before repo; the lint step comes last.

**Orphans.** An orphan is a file with a `Code generated by nano gen` header, under an output directory of a step that ran, that the run did not produce. Generate deletes it; `-check` fails on it. A deleted mapper declaration cannot leave generated code behind.

**How to review it:** read the literal values in `nanogen/project.go`. Rely on CI for every output. When a service adopts the pipeline, these are deleted: `generate_*.sh`, both `dbgen.go` files, the `*codegen.yaml` files, the `//go:generate` lines in `cmd/app` and the `Run(Check)` tests.

---

## 10. Repositories: `nano gen repo`

**Status: on main**, unused by the services. One repository interface per repository: each method is hand-written, or derived from its name, or the run fails at the method (design PRs 7 and 8, [ADR 0026](adr/0026-nano-gen-repo.md)). Reference: [Repositories](generation.md#repositories-nano-gen-repo). Fixture: [`gen/repo/internal/repotest`](../gen/repo/internal/repotest), whose integration tests run every helper of the base and every derived method on PostgreSQL, against the hand-written body a developer would write.

### What you write

The interface, one declaration and the implementation, which embeds the base. Methods without a body here are derived. From the fixture's `repository.go`:

```go
type ItemRepository interface {
	FindByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error)
	GetByTenantIDAndID(ctx context.Context, tenantID TenantID, id string) (Item, error)
	DeleteByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (bool, error)
	CountInternal(ctx context.Context) (int64, error)
	// Hand-written: also derivable, but compares the name case-insensitively.
	ExistsByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (bool, error)
	ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error)
	// …
}

func _() {
	nanorepo.Implement[ItemRepository, itemRepositoryImpl](table.Items, itemToDomain)
}

type itemRepositoryImpl struct {
	itemsBase
}

func (r *itemRepositoryImpl) ListActive(ctx context.Context, tenantID uuid.UUID) ([]Item, error) {
	return r.findAll(ctx, r.where.TenantID(tenantID).AND(r.where.Status(model.ItemStatus_Active)),
		r.t.Sku.ASC(), r.t.ID.ASC())
}
```

The grammar: `FindBy`, `GetBy`, `ExistsBy`, `CountBy`, `DeleteBy`, `FindAll`, `Count`; columns by their Go field name joined by `And`, equality only; `ctx` first, then one parameter per column in name order. A single-row find derives only when its columns cover a key in `UniqueKeys()`. On a scoped table the name starts with a scope column, or ends in `Internal` on a read.

### What is generated

`repo_base.gen.go`, one per declaring package: per table, a struct with the database, `t` and `where`, a constructor, seven helpers and the transactor methods. The fixture declares three tables; its base is 1,124 lines, most of them predicates. `findOne` is the method to know:

```go
// findOne returns the row matching cond: absent when none does, and
// transactor.ErrMoreThanOneRow when two do. It reads LIMIT 2, so a key that
// stopped being unique is an error, never a silent first row. To pick one of
// several rows, use first with an order.
func (b itemsBase) findOne(ctx context.Context, cond postgres.BoolExpression) (functional.Option[Item], error) {
	stmt := b.t.SELECT(b.t.AllColumns)
	if cond != nil {
		stmt = stmt.WHERE(cond)
	}
	rows, err := b.queryRows(ctx, stmt.LIMIT(2)).Value()
	switch {
	case err != nil:
		return functional.None[Item](), err
	case len(rows) > 1:
		return functional.None[Item](), fmt.Errorf("%w: items", transactor.ErrMoreThanOneRow)
	case len(rows) == 0:
		return functional.None[Item](), nil
	}
	return functional.Some(rows[0]), nil
}
```

`<file>_repo.gen.go`, one per declaring file with a `nanorepo.Implement`: the `var _` line of each binding and its derived methods, each one helper call with the SQL in its comment. From the fixture's `repository_repo.gen.go`:

```go
// ItemRepository on *itemRepositoryImpl: 11 derived methods on table items; the other methods are
// hand-written (nano gen repo -explain lists each one).
//
// Hand-written although the name derives (deleting the method switches it
// to the derived query): ExistsByTenantIDAndName.
//
// A compile error on the next line means ItemRepository gained or lost a method since
// the last run: run nano gen repo.
var _ ItemRepository = (*itemRepositoryImpl)(nil)

// FindByTenantIDAndSku is derived from its name:
//
//	SELECT * FROM items WHERE tenant_id = $1 AND sku = $2 LIMIT 2 -- at most one row: items_tenant_sku_key (tenant_id, sku); a second row is ErrMoreThanOneRow
func (r *itemRepositoryImpl) FindByTenantIDAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error) {
	return r.findOne(ctx, r.where.TenantID(tenantID).AND(r.where.Sku(sku)))
}

// CountInternal is derived from its name:
//
//	SELECT COUNT(*) FROM items
//
// UNSCOPED: the name ends in Internal, so it does not filter on tenant_id.
func (r *itemRepositoryImpl) CountInternal(ctx context.Context) (int64, error) {
	return r.count(ctx, nil)
}
```

`go tool nano gen repo -dir internal/repository -scopes tenant_id -explain` prints how each method resolves, and writes nothing:

```text
ItemRepository on *itemRepositoryImpl (table items, scope tenant_id)
  FindByTenantIDAndSku           derived: SELECT * FROM items WHERE tenant_id = $1 AND sku = $2 LIMIT 2 -- at most one row: items_tenant_sku_key (tenant_id, sku); a second row is ErrMoreThanOneRow
  CountInternal                  derived, UNSCOPED (Internal): SELECT COUNT(*) FROM items
  ExistsByTenantIDAndName        hand-written repository.go:98
                                 note: the name also derives (SELECT COUNT(*) FROM (SELECT 1 FROM items WHERE tenant_id = $1 AND name = $2 LIMIT 1) found); the hand-written method wins
  ListActive                     hand-written repository.go:106
```

### Errors an agent sees

Each at its line, all in one run, one golden per rule in [`gen/repo/testdata/problems`](../gen/repo/testdata/problems). A method that is not hand-written and does not derive names why, and gives the signature to paste and, when one works, the rename:

```text
derive.go:43: Items.FindByTenantIDAndName: not implemented: no hand-written method on *itemsImpl, and the name does not derive: it returns one row, but (tenant_id, name) covers no unique key; the keys of items are items_pkey (id), items_tenant_sku_key (tenant_id, sku)
  fix: return []Item; or add a unique index on (tenant_id, name) in a migration and rerun nano gen db;
       or write it by hand on *itemsImpl, in a non-generated file of package derive:
         func (r *itemsImpl) FindByTenantIDAndName(ctx context.Context, tenantID uuid.UUID, name string) (functional.Option[Item], error) {
             return r.first(ctx, r.where.TenantID(tenantID).AND(r.where.Name(name)), r.t.CreatedAt.ASC(), r.t.ID.ASC())
         }
derive.go:38: Items.FindByTenantIdAndSku: not implemented: no hand-written method on *itemsImpl, and the name does not derive: no column "TenantId" on table items
  fix: write it by hand on *itemsImpl, in a non-generated file of package derive:
         func (r *itemsImpl) FindByTenantIdAndSku(ctx context.Context, tenantID uuid.UUID, sku string) (functional.Option[Item], error)
       or rename it FindByTenantIDAndSku (did you mean TenantID?)
```

The other rules: the grammar, an operator (`IgnoreCase`, `In`, `OrderBy`, `Or`, with the hand-written body), the parameters (count, pointer, a name that says another column, the type), the results, the mapper's domain type, the tenant scope (`Internal` skips it on reads only), a hand-written or promoted method whose signature differs, an ambiguous promotion, an implementation that does not embed its base, a table package without `UniqueKeys` (run `nano gen db`), a column a migration renamed (the rename first), and the package's own compile errors. The scope warning on hand-written code keeps exit 0:

```text
warning: internal/repository/users.go:31: ByEmail: condition on users has no tenant_id; this query can read or change another tenant's rows
  fix: add u.where.TenantID(…) to the condition, or name it ByEmailInternal for a trusted system path
```

Before `nano gen` runs again, the compiler shows two messages:

- `cannot use (*impl)(nil) … (missing method X)` at the `var _` line of `<file>_repo.gen.go`: the interface gained `X`, or its hand-written body was deleted. Run `go tool nano gen`; it derives `X` or names the fix.
- `method impl.X already declared at <file>_repo.gen.go:N`: you hand-wrote a method that the last run derived. Run `go tool nano gen`; the hand-written method wins and the derived copy goes.

### Adopting it in a service

1. Add `Scopes` to the `Postgres` declaration and run `go tool nano gen`. Read every scope warning: each is a finder that may cross tenants, or a trusted path to rename `…Internal`.
2. Declare one repository, embed its base, set it in the constructor, and run `go tool nano gen repo -explain`: it says which methods would derive. Delete the hand-written body of each one that does, one file at a time.
3. Rename the finders whose names hide a filter: a derived name says every column it filters on (`FindByExternalID` becomes `FindByProductIDAndExternalID`, `TenantID` becomes the column's field, `PlatformTenantID`). A team that wants the short name keeps a one-line hand-written body.
4. Every hand-written `LIMIT 1` on columns that are not unique becomes `first` with an order that ends on a unique column. `findOne` and the derived single-row finds return `transactor.ErrMoreThanOneRow` instead of an arbitrary row; handle it with `errors.Is` where the caller can see duplicates. A derived `GetBy…` returning `(D, error)` returns `sql.ErrNoRows` on no row.
5. Keep inserts, upserts and statements the helpers do not cover hand-written on `r.t` with `r.queryRow` or `r.exec`, which keep `OnUnique` and the transaction.

### How to review it

- **Read the interface and the hand-written methods**, not the generated files. A derived method's name is its contract: it cannot hide a filter.
- **Read the SQL comment** of each new derived method, or the `-explain` output, and the `Hand-written although the name derives` line above each `var _`: a deleted body there switches the method to the derived query.
- **Read every scope warning** and every new `…Internal` name: it states that the path is trusted. A derived `Internal` method is a read; a delete across tenants is always hand-written.
- **Check each `first`** for an order that ends on a unique column.
- **Do not review `repo_base.gen.go`** beyond its predicate list; `-check` proves both files match the declarations.

### Searches

**Status: on main**, unused by the services ([ADR 0027](adr/0027-search-in-the-db-module.md), [design](design/search.md)). A search is a declaration next to `nanorepo.Implement`. `nano gen repo` proves its tenant scope, and writes a typed builder, the repository method and, with `API[Body]`, the mapper from the OpenAPI body. PostgreSQL, MySQL, MariaDB and SQLite run it.

**What you write.** One `nanorepo.Search[Row]` call per search in `func _()`, and one interface method that takes the builder. From the fixture `gen/repo/testdata/search/search.go`:

```go
func _() {
	nanorepo.Implement[ItemRepository, itemRepositoryImpl](table.Items, itemToDomain)
	nanorepo.Search[model.Items]("ItemBySku", table.Items,
		nanorepo.Map(summarize),
		nanorepo.Scope(table.Items.TenantID),
		nanorepo.Filter(table.Items.Status, nanorepo.Eq, nanorepo.In),
		nanorepo.FilterAs[sku](table.Items.Sku, nanorepo.In.API("skus")),
		nanorepo.Filter(table.Items.CreatedAt, nanorepo.Range("created_since", "created_until").HalfOpen()),
		nanorepo.Sort(table.Items.Name, table.Items.CreatedAt),
		nanorepo.Tiebreak(table.Items.Sku, table.Items.ID),
		nanorepo.DefaultSort(table.Items.CreatedAt, nanorepo.Desc),
		nanorepo.DefaultLimit(50),
	)
}

type ItemRepository interface {
	SearchBySku(ctx context.Context, q ItemBySkuSearch) (search.Result[itemSummary], error)
	// …
}
```

`search` is `db/search`. The options: the tenant scopes (`Scope`, `ScopeOrNull`, `ScopeNull`, `ScopeFunc`, `Unscoped`), the joins (`From`), the filters (`Filter`, `FilterAs`, `FilterExpr`, `FilterExists`, `Flag`, `FullText`, `GoOnly`), the sorts (`Sort`, `SortKey`, `SortExpr`, `SortHandWritten`, `Tiebreak`, `DefaultSort`), the mapper (`Map`, `MapPage`) and the API binding (`API[Body]`, `IgnoresFullText`, `APIHandWritten`). The `db/nanorepo` package doc gives each one.

**What is generated**, in `<file>_repo.gen.go`:

- `ItemBySkuSearch`, an immutable builder: `NewItemBySkuSearch(scope)`, one typed method per filter (`q.Status().In(...)`, `q.CreatedAt().Since(t)`), `SortBy`, `Limit`, `Offset`, `After` and `FullText`. With more than one scope field, the constructor takes a scope struct (`ItemOfTenantScope`) with keyed fields only; a zero field refuses the search with `search.ErrUnscoped`. `q.Scope()` returns the scope value.
- `ItemBySkuSort`, the sort field type, with one constant per sort.
- The spec variable (`itemBySkuSearch = searchexec.DefineSpec(...)`): the read set, the scope, the keys and the tiebreak.
- The repository method, one line on the base: `return r.search(ctx, itemBySkuSearch, q.Query, r.summarize)`.
- With `API[Body]`: `ItemBySkuSearchFromAPI(scope, body *Body) ItemBySkuSearch`. When the handler of the body is in another package, it goes there, in `<search>_search_api.gen.go`.

`nano gen` also writes `nanogen/search-exemptions.txt`: one line per `Unscoped`, `ScopeOrNull`, `ScopeNull`, coverage through a referenced row, `IgnoresFullText` and `APIHandWritten`. `-check` reports it stale.

#### Tenant scopes

The tenant columns are declared **once per project**, in `nanogen/project.go`, by column name. They apply to every table that has a column of that name. Echopoint's declaration:

```go
Postgres: &project.Postgres{
	Scopes: []string{"organization_id", "workspace_id"},
	ContextScopes: []project.ContextScope{
		{Column: "workspace_id", Func: "echopoint/internal/db/workspacescope.Filter"},
	},
},
```

- A column of `Scopes` that is not in `ContextScopes` is a **hard scope** (`organization_id`). The caller passes its value: it is a parameter of `New<Search>Search(...)`, or a field of the scope struct. No function can supply it.
- A column of `ContextScopes` is a **context scope** (`workspace_id`). The function registered for it reads the value from the request context: `func(context.Context, C) B`. The function may return TRUE, which means a read across the column, such as an organization-wide read when no workspace is selected. A project registers one function per column, so every search filters that column the same way.
- A `ContextScopes` column that is not in `Scopes`, a column listed twice, or a function that is not written as `importpath.Func` is a project error.

**Where the declaration applies.**

| Code | Which columns count | Effect |
|---|---|---|
| A search (`nanorepo.Search`) | Every column of `Scopes` on every table instance the search reads, **nullable or not** | Each one must be covered, or `nano gen` fails (E9, E10). |
| Derived methods and hand-written queries on the base | Only **NOT NULL** columns of `Scopes` | A derived method must filter on the scope column first, or end in `Internal` on a read. A hand-written query that does not filter on it is a warning, not an error. |

So a nullable `workspace_id` changes nothing for derived methods and hand-written finders. Only searches must cover it.

**How each column is covered.**

| Column | Covered by | Generated code |
|---|---|---|
| hard (`organization_id`) | `Scope(table.T.OrganizationID)` | `NewTSearch(organizationID uuid.UUID)`, and in the spec `searchexec.ScopeEq(..., table.T.OrganizationID)`: `WHERE organization_id = $1` on every page and count |
| context (`workspace_id`) | `ScopeFunc(workspacescope.Filter, table.T.WorkspaceID)` | In the spec `Context: []searchexec.ContextScope{searchexec.ScopeFunc(workspacescope.Filter, table.T.WorkspaceID)}`: the function's condition on every page and count. The constructor takes no workspace. |
| a joined table | an `INNER JOIN` on a declared foreign key from a covered instance, or the tenant column equated in the `ON` clause | nothing extra: `-explain` names the rule |
| a shared row (`tenant_id IS NULL`) | `ScopeOrNull` or `ScopeNull` | `(column = $1 OR column IS NULL)` or `column IS NULL`, listed in the exemption file |
| a deliberate exemption | `Unscoped(column, "why")`, only when another **hard** column of the same table stays covered | nothing in SQL; the exemption file lists the column and the reason |

**Recipes.**

```go
// 1. A table with organization_id only (variables): one scope.
nanorepo.Search[model.Variables]("Variable", table.Variables,
	nanorepo.Scope(table.Variables.OrganizationID),
)

// 2. A table with both columns, read in the selected workspace, or the
//    whole organization when the request selects none (the usual case).
nanorepo.Search[model.Webhooks]("Webhook", table.Webhooks,
	nanorepo.Scope(table.Webhooks.OrganizationID),
	nanorepo.ScopeFunc(workspacescope.Filter, table.Webhooks.WorkspaceID),
)

// 3. A table with both columns, always read across the whole organization,
//    even when a workspace is selected (an admin report).
nanorepo.Search[model.Webhooks]("WebhookOrgWide", table.Webhooks,
	nanorepo.Scope(table.Webhooks.OrganizationID),
	nanorepo.Unscoped(table.Webhooks.WorkspaceID, "organization admin report across workspaces"),
)
```

Recipes 2 and 3 are tested on PostgreSQL: see `ProjectInWorkspace` and `ProjectOrgWide` in [`gen/repo/internal/repotest/tenant.go`](../gen/repo/internal/repotest/tenant.go), on a table with a NOT NULL `tenant_id` and a nullable `workspace_id`. Recipe 2 reads one workspace when the context selects one, and the whole tenant when it selects none. Recipe 3 reads the whole tenant in both cases. Neither ever reads another tenant's rows.

The call site:

```go
q := NewWebhookSearch(principal.OrganizationID). // from the authenticated principal, never from the body
	Name().Contains(term).
	SortBy(WebhookSortCreatedAt, search.SortDescending)
```

**What the generator refuses.** Each error is at the declaration line, with the fix:

| You write | Error |
|---|---|
| Only `Scope(OrganizationID)` on a table that also has `workspace_id` | E9 `workspace_id is a tenant column and no rule covers it: it would read every tenant's rows`. Fix: `ScopeFunc(workspacescope.Filter, ...)`, or `Unscoped(..., "why")` (recipe 3) |
| `ScopeFunc(fn, table.T.OrganizationID)` | E9 `organization_id is a hard tenant column: its value comes from the scope value, never from a function, which could return TRUE` |
| `ScopeFunc(otherFn, table.T.WorkspaceID)` | E9 `otherFn is not the function that Postgres.ContextScopes registers for workspace_id` |
| `Scope(table.T.WorkspaceID)` | `workspace_id is a context tenant column (Postgres.ContextScopes): only its registered ScopeFunc scopes it` |
| `Unscoped(table.T.OrganizationID, ...)` with no other hard column covered | E30 `leaves T with no other covered hard tenant column`: a search is never unscoped as a whole |
| A joined table with no tenant column and no foreign key path to a covered table | E10 |
| A `Scope` in `WHERE` on the nullable side of a `LEFT JOIN` | E29: it would drop the rows with no match. Put the tenant in the `ON` clause instead. |
| Two `Scope` calls that make one field name | E12: give both columns to one `Scope` |

**One limit to know.** A table whose only tenant column is a context column relies entirely on the registered function. If the function returns TRUE (no workspace in the context), such a search reads every row of the table. Give such a table a hard tenant column as well.

The full rules (the five coverage rules, subqueries, correlations, the exemption file) are in [the search design](design/search.md), section 6. The `db/nanorepo` package doc gives each option.

**A hand-written search method** that needs more than the declaration (a computed status, a mapper with a run-time option) starts from the plan, and runs it with the transactor:

```go
func (r *webhookRepositoryImpl) Search(ctx context.Context, q WebhookSearch) (search.Result[webhook.Webhook], error) {
	plan, err := searchexec.Resolve(ctx, webhookSearch, q.Query)
	if err != nil {
		return search.Result[webhook.Webhook]{}, err
	}
	order, err := plan.Order(map[WebhookSort]keyset.Key[webhookRow]{WebhookSortRequestCount: requestCountKey})
	if err != nil {
		return search.Result[webhook.Webhook]{}, err
	}
	return transactor.RunPlan(ctx, r.db, plan, order, r.toDomain).Value()
}
```

`plan.And` narrows the condition; nothing widens it. Do not call `plan.Run` with an executor of your own: `transactor.RunPlan` and `RunPlanPage` use the context transaction and refuse a row lock.

**Errors an agent sees.** Each one is at the declaration line, with a fix. The most common:

- An uncovered tenant column (E9) or table instance (E10): add a `Scope` (hard column) or the registered `ScopeFunc` (context column), join it on a declared foreign key, or write `Unscoped(column, "why")` when another hard column of the table stays covered. See [Tenant scopes](#tenant-scopes).
- A table with no primary key and no `Tiebreak` (E3), or a `Tiebreak` that covers no unique key (E4): add the columns of one unique key to `Tiebreak`.
- An interface method that takes the builder with another signature (E20), or a hand-written method that does not start from `searchexec.Resolve` with its own `q` (E19).
- A declared search that no method takes (E24).
- With `API[Body]`: a `sort_by` value with no sort (A1), a body property that no option reads (A10), or a page size or `maxItems` that the runtime does not allow (A9).

`nano gen repo -explain` prints, per search, how each table instance is covered and how each body property binds.

### Adopting searches in a service

Do one search per pull request. Each step is a check for the author and for the reviewer.

1. **Framework.** Require root, `db` and `gen` at the same release. The db step must write `ColumnTypes()` and `ForeignKeys()`: run `go tool nano gen -fresh` once. Set `Postgres.Scopes` to every tenant column, and `Postgres.ContextScopes` for a column that a function scopes from the context ([Tenant scopes](#tenant-scopes)). Adding a nullable column to `Scopes` affects searches only.
2. **Bind the repository** with `nanorepo.Implement` and embed its base, if it is not bound yet. The generated search method calls the base.
3. **Declare the search** next to `Implement`. Start with the filters, sorts and tiebreak of the hand-written query. Run `go tool nano gen repo -explain` and read the coverage of each instance.
4. **Scope.** The handler builds the scope value from the authenticated principal, never from the body. With a scope struct, write keyed fields. In hand-written code, read the scope back with `q.Scope()`, not from a second parameter.
5. **Exemptions.** Commit `nanogen/search-exemptions.txt`. Each line is a decision the reviewer approves: an `Unscoped` reason, a shared row, `IgnoresFullText`, `APIHandWritten`.
6. **`full_text_search` changes.** A generated full text ignores case, matches `%`, `_` and `\` literally, trims the term, and ignores a blank term. A new `FullText` filters where nothing filtered before. Write it in the product release note.
7. **Gate grep.** Before the pull request merges, export the product's gate flows from the prod Echopoint CI org (`echopoint --profile prod flows get <id> -o json`) and grep for `full_text_search`, the range properties of the search, its sort cursors, and arrays or terms above `search.MaxInValues` or `search.MaxTermLength` (1000). Update each flow that asserts the old result, and run `flows publish <id>` where a published version exists.
8. **`x-go-type-import`.** Where the OpenAPI spec maps `search.Pagination` or `search.SortDirection` with `x-go-type`, change `x-go-type-import` from `pkg/search` to `db/search` and regenerate. `pkg/search` holds aliases of `db/search`, so both compile during the move.
9. **Delete the hand-written query** and its tests that the generated method replaces. Keep a database test that compares one page of the old and the new method on the same rows until the gate passed.

A search that has no SQL builder (a remote call, data in memory) keeps its hand-written code with `db/search.Request`.

---

## 11. Prototypes

The search prototype lives in the workspace `reports/prototypes/derived-repo/`, which another agent is still extending. The excerpts below come from the written report, `reports/prototype-derived-repositories.md`, which is a fixed snapshot. The prototype's current files have moved on; each section says how.

### Derived names: `nano gen repo`

Landed: see [section 10](#10-repositories-nano-gen-repo). The prototype report, `reports/prototype-derived-repositories.md`, stays as the record of the design's evidence: 55 database tests on anchor's migrations compare each derived method with anchor's own.

### Search builder

Landed: see [Searches](#searches). The design is [docs/design/search.md](design/search.md). The prototypes in the workspace `reports/prototypes/search-*` stay as the record of the design's evidence.

---

## Worked trace: `GET /v1/products/{product_id}/product-users/{product_user_id}` in anchor

One request, from the spec to the database and back. **G** marks a generated file, **H** a hand-written one. All paths are in anchor `apps/anchor`.

1. **H** `cmd/http/openapi.yaml:7131-7143`: the path, its two parameters, `operationId: getProductUser`, `tags: [ProductUser]`, and the `product_user:read` scope.
2. **G** (oapi-codegen) `internal/api/gen.go:10449`: the chi route `r.Get(options.BaseURL+"/v1/products/{product_id}/product-users/{product_user_id}", wrapper.GetProductUser)`.
3. **G** `internal/api/gen.go:9401`: `ServerInterfaceWrapper.GetProductUser` binds `product_id` and `product_user_id`. A binding failure goes to `ErrorHandlerFunc` as `InvalidParamFormatError`.
4. **G** `internal/api/gen.go:25511`: `strictHandler.GetProductUser` builds `GetProductUserRequestObject`, runs the strict middlewares, and calls the handler. An error goes to `ResponseErrorHandlerFunc`.
5. **H** `cmd/http/server.go:275-297`: mounts `api.NewStrictHandlerWithOptions` with anchor's own `errorMiddleware.HandleRequestError` and `HandleResponseError` (the legacy envelope, not the nano problem writers).
6. **H** `internal/api/product_api.go:361`: `(*AnchorAPI).GetProductUser` builds `user.FindProductUserInput` by hand and calls the service.
7. **H** `internal/service/product_user_service.go:82`: `Find` validates the input and calls the repository.
8. **H** `internal/repository/product_user_repository.go:88`: `FindByProductIDAndID` builds the SELECT with the generated table and calls `transactor.QueryOptionalMap`.
9. **G** (go-jet) `internal/db/gen/anchor/public/table/product_users.go`: `table.ProductUsers.AllColumns`, `.ID`, `.ProductID`.
10. **G** (go-jet) `internal/db/gen/anchor/public/model/product_users.go`: the row is scanned into `model.ProductUsers`.
11. **H** `internal/mapper/product_user_mapper.go:18`: `ToDomain` turns `Name *string` into `""` and casts `Status` without a check.
12. Back up: the service turns `None` into `user.ErrProductUserNotFound`; the handler returns `GetProductUser200JSONResponse(mapProductUserToResponse(user))`; the generated visitor writes the JSON.

Two generated files assembled this chain at start-up:

- **G** (nano gen wire) `cmd/app/wire.gen.go`: `productUserMapper = mapper.NewProductUserMapper()` (`:1235`), `repository4.NewProductUserRepository(db2, productUserMapper, logger)` (`:1244`), `service8.NewProductUserService(…)` (`:1899`), `api.NewAPI(…)` with its 31 arguments (`:2144`), and `httpserver.NewHandler(ctx, cfg.Server, …, anchorAPI, …)` (`:2183`).
- **G** (nano gen config) `cmd/app/config.gen.go`: binds the `postgres` section the database pool uses and the `server` section the handler uses.

What changes when the designed pieces land:

| Step | Today | After |
| --- | --- | --- |
| 3-4 | Upstream chi wrapper, no parameter validation | nano template: `validateParams` runs before the handler (validate) |
| 6 | `(*AnchorAPI).GetProductUser` on one 31-parameter struct | `productUserHandler.GetProductUser` behind a generated `ProductUserServer` (tag split) |
| 11 | `(*ProductUserMapper).ToDomain`, wired as a provider | Generated `ProductUserToDomain(model.ProductUsers) (user.ProductUser, error)`, which rejects an unknown status (mapper) |
| 8 | Hand-written `FindByProductIDAndID` | Derived from its name (`nano gen repo`, on main) |
| wire | 23 mapper provider blocks | None: generated mappers are plain functions |

---

## What to read by hand, and what never

| Read by hand | Never read by hand (`-check` owns it, or will) |
| --- | --- |
| SQL migrations; one pass over the model diff with the checklist in section 1 | go-jet alias helpers, `EXCLUDED`, `*_use_schema.go` |
| `openapi.yaml`, `codegen.yaml` or `oapi-codegen.yaml` | `gen.go`, `api.gen.go` wrapper bodies and visitors |
| The spec diff, the validate gap list, every `-allow` | `validate.gen.go` per-field blocks |
| The `//nano:errors` interface and the `ERRORS.md` diff | `errors.gen.go` |
| `nanomap` declarations: waiver reasons, `Arg` order, `Reverse` rules; changed call sites | `mapper.gen.go` |
| Tags in `openapi.yaml`; each handler's constructor | `handlers.gen.go` |
| `Config` struct tags and the `CONFIG.md` diff | `config.gen.go`, `.env.example` |
| `manifest.go`, `module.go`, and the seven-item `wire.gen.go` checklist | `need*` chains, `Overrides`, `Components` |
| Literal values in `nanogen/project.go` | Headers, orphans, `nanogen.sum` |
| Repository interface method names, `nanorepo.Implement` declarations, hand-written repository methods; search declarations and `nanogen/search-exemptions.txt` | `repo_base.gen.go`, `<file>_repo.gen.go` beyond their SQL comments and `-explain` |
