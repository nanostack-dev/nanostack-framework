# 0026: `nano gen repo` binds one repository interface to a go-jet table, on a generated base

- Status: accepted
- Date: 2026-11-08

## Context

A Nanostack repository is an interface, an implementation struct and hand-written go-jet code. Each finder passes the database and the mapper to a `pkg/db/transactor` function, writes the same `WHERE tenant = $1 AND id = $2` and the same `LIMIT 1`, and repeats the no-rows handling. Anchor's product-user and platform-user repositories hold nine such methods in 156 lines. A `LIMIT 1` on columns that are not unique returns whichever row PostgreSQL reads first, and nothing reports it.

The user's rule for part 1 (2026-11-08): "spring data like method following a standard that generate code if the method is not handmade; if the interface has a method he can't generate and found no implementation we fail in build." A raw-SQL directive (`//nano:query`) is dropped: go-jet stays the one way to write SQL. The design (`reports/design-repo.md` in the workspace, decisions D1 to D27) was prototyped on anchor's code: 55 database tests compare the derived methods with anchor's own.

The design ships in three PRs: the db step with `UniqueKeys()` (PR 6, [decision log](../decision-log.md#2026-11-08-nano-gen-db-writes-the-models-uniquekeys-and-nanogensum)), the base (PR 7, this record) and the derived methods (PR 8).

## Decision

1. **One declaration binds an interface to its implementation and a table (D2, D3).** `nanorepo.Implement[Iface, impl](table.X, toDomain)` inside `func _() { … }`, in a file without a build tag, so the compiler, `go vet` and gopls check it and a rename updates it. The model row and the domain type come from `toDomain`. `nanorepo.NoMapper[model.X]` returns the model rows; `nanorepo.Base[model.X](table.X)` declares a base with no interface.
2. **The declarations live in `db/nanorepo` (D4).** It imports only the standard library, and its functions do nothing at run time. The root module never requires `gen`, and a service imports the declarations, so they cannot live in `gen`.
3. **Each table a declaration names gets a generated base (D15).** `repo_base.gen.go` in the declaring package holds, per table, `<table>Base` with the table (`t`), typed predicates (`where`), helpers that take a condition (`findOne`, `first`, `findAll`, `exists`, `count`, `deleteWhere`, `updateWhere`) and every `pkg/db/transactor` function as a method, generic where the function is (`query[T]`, `queryOptional[T]`, `queryMap`, `queryOptionalMap`, `queryMapSlice`, `queryCount`, `exec`, `execAffected`, `page`, and `queryRow`, `queryRowOptional`, `queryRows`, which map with the declaration's mapper). The implementation embeds the base and sets it with `new<Table>Base(db)`.
4. **No silent first row (D16, D17).** `findOne` reads `LIMIT 2` and returns `transactor.ErrMoreThanOneRow`, wrapped with the table name, when two rows match. `first` takes at least one order. The sentinel lives once, in `pkg/db/transactor`, so `errors.Is` matches across repository packages.
5. **Every helper runs through `pkg/db/transactor` (D18).** The context transaction and the row-lock rules hold: `findOne`, `first` and `findAll` honour `transactor.ForUpdate`; PostgreSQL refuses a lock on `exists` and `count`, which aggregate; `deleteWhere` and `updateWhere` run through the new `transactor.ExecAffected`, which refuses a lock as `Exec` does and returns the affected rows. A nil condition on a write is an error, not a write to every row.
6. **The mapper takes no state (D5, D6).** It is a function, or a variable of the declaring package set to a function, a function literal or a method value on a type without fields. A method value on a type with fields is an error: the base calls the mapper with no dependencies. Two declarations on one table in one package share its base, so they pass the same mapper and read the same model.
7. **PostgreSQL only in part 1 (D20).** A declaration on a MySQL or SQLite table is an error whose fix is to remove it; the repository stays hand-written.
8. **The scope columns come from the project file (D25, D26).** `Postgres.Scopes` names the tenant columns. A table is scoped when it has one as a `NOT NULL` column. A hand-written function that touches a scoped table through its base (a predicate, a helper, a `SELECT`, `UPDATE` or `DELETE` on `b.t`, or a `SELECT` that reads `FROM` `b.t` or a join on it) without a predicate or a reference on a scope column gets a warning, unless its name ends in `Internal`. Warnings keep exit 0 and reach `-json` as `warning` events. Derived names (PR 8) must filter on the scope first.
9. **The base is all or nothing (D27).** The generator loads the package with `repo_base.gen.go` replaced by an empty file, so a stale base never blocks a run, then type-checks the package with the new base. A compile error that remains is reported at its line, and nothing is written.
10. **The repo step runs after errors and before config, and needs db (D27).** It reads the table code and its `UniqueKeys()` from Go source, never a database, so `-only repo` and `-skip db` run without Docker. A failed db step skips it, and config needs it. `-list` shows one unit per declaring package.

## Alternatives

- **`//nano:query` with SQL text.** Dropped by the user: it removes the purpose of go-jet, and it needed a database, plan analysis and a new executor in the repo step.
- **A `//nano:repo impl=… table=…` comment.** It is untyped, and a rename does not update it.
- **Naming conventions** (`XRepository` → `xRepositoryImpl`). Anchor's `resourcetype` uses `Repository` → `repositoryImpl`.
- **Hand-written methods that pass `r.db` and the mapper to transactor functions**, as anchor does. Each finder repeats both.
- **`LIMIT 1` in `findOne`.** It hides a missing unique key.
- **One `ErrMoreThanOneRow` per generated package.** `errors.Is` across two repository packages would not match.
- **Package-level generic functions that take the base**, instead of generic methods. Each call repeats the base.
- **Any method value as a mapper.** A zero-value mapper with state panics: anchor's `IntegrationInstanceMapper.ToDomain` calls its cipher.
- **`nanorepo.Scope` per declaration**, as the prototype had. A declaration can leave it out.
- **The scope check as an error now.** Anchor's hand-written finders need a review first; it becomes an error once anchor's `Internal` methods are clean.
- **Partial output on error.** A base written while another declaration fails would leave the package half generated; the tree keeps the last good files.

## Consequences

- A service declares `Postgres.Scopes`, embeds a base per table and moves its finders onto `r.findOne`, `r.first` and `r.where`, one repository file at a time, after PR 8. Every hand-written `LIMIT 1` on columns that are not unique moves to `first` with an order.
- The base needs Go 1.27 for its generic methods. Go 1.27.0 to 1.27.3 lose the order of generic methods in export data (golang/go#81188), so a service that adopts the base keeps the framework's `TEMP(golang/go#81188)` lint exceptions until Go 1.27.4 ([decision log](../decision-log.md)).
- String predicates compare as text (`$1::text`). On a column of another type that the model reads as a string, such as `json`, `inet` or `tsvector`, the predicate fails at run time; such a column is compared on `b.t`. Enum columns cast to their type, so their index applies.
- Until PR 8, every method of the bound interface is hand-written on the base; the interface itself is checked only for being an interface.
