# 0027: Search in the db module

- Status: accepted
- Date: 2026-11-09

## Context

A Nanostack search is a POST endpoint that takes filters, a sort, a full-text term and a page, and returns `search.Result`. Each one is hand-written today: a `pkg/search.Request` in the domain, go-jet code in the repository, and the keyset pager of `pkg/db/transactor` ([ADR 0001](0001-keyset-cursor-pagination.md)). echopoint has 30 such searches and anchor has 24. Each repository writes its own tenant condition, its own substring rule and its own API mapping, and nothing checks that a search reads only one tenant's rows.

Alexis approved the rework on 2026-11-09: a typed builder that `nano gen repo` generates ([ADR 0026](0026-nano-gen-repo.md)), with four additions (the search method on the repository implementation, where a hand-written method wins; every tenant column of every join scoped, with the API body and the declaration checked both ways; generic field types with a strict `In()`; the transactor's keyset pager with safe limits and `CheckPage`). The design is `reports/design-search.md` in the workspace (decisions S1 to S14, after four reviews). It lands in the repository as `docs/design/search.md` with the last generator PR (G14).

The search must live in the db framework, not beside it. [ADR 0015](0015-fork-go-jet-as-the-db-module.md) makes the db module the only database library, on PostgreSQL, MySQL/MariaDB and SQLite. The db module requires no module of this repository other than `pkg/functional`, so it cannot import the root module, where the context transaction lives (`transactor.WithTx`, `CurrentTx`). `db/exec`, the executor seam of ADR 0015 stage 2, does not exist yet.

This record covers the decisions that shape the modules: S1 (packages), S3 (dialects), S5 and S6 (tenant coverage), S7 (column types), S10 (runtime checks) and S11 (the move from `pkg/search`). The design gives the API, the gen errors and the PR plan.

## Decision

1. **Four packages in the db module (S1).**

   | Package | Holds | Imports |
   |---|---|---|
   | `db/search` | The contract: `Result`, `Pagination`, `Sort`, the cursor, the errors, the limits, `CheckPage`. `Request` joins it in G14. | Standard library only |
   | `db/keyset` | The one keyset pager: keys, `Order`, `Seek`, `Cursor`, `Page`. It moves from `pkg/db/transactor`. | `db/search`, `db/query`, the dialect packages |
   | `db/searchexec` | The runtime of generated searches: `Spec`, `Query`, `Resolve`, `Plan`, `Run`, the field types, `Contains`. | `db/keyset`, `db/search`, `db/query` |
   | `db/nanorepo` | The search declarations (`nanorepo.Search` and its options), next to `nanorepo.Implement`. | Standard library only |

   Domain packages import `db/search` and get no SQL dependency, as the 2026-05-22 decision "Search Contract Split" requires.

2. **The executor is a parameter, typed `query.Executor` (S1).** `db/keyset` and `db/searchexec` never read a transaction from the context. They take the executor as `db/query` does, and every statement runs through `db/query` (`query.All`, `query.Count`), not through its own scan call. R4 adds the alias `type Executor = qrm.Queryable` to `db/query`; `db/query` takes `qrm.Queryable` directly today. `pkg/db/transactor` passes `transactor.Executor(ctx, db)` in, applies its row-lock rules, and becomes thin forwarders (`transactor.Search`, `transactor.Page`, the key aliases).

3. **No `db/exec` now; stage 2 changes one alias and one scan path (S1).** Search runs on `database/sql` through `db/query`, as the transactor's pager runs today. When `db/exec` lands, the signatures of `keyset.Page`, `searchexec.Run` and `transactor.Search` change type, because `qrm.Queryable` returns `*sql.Rows`, which stage 2 replaces. The alias and the routing through `db/query` keep that change to one alias and one scan path. The pager moves once, and does not move again.

4. **One substring rule on three dialects (S3.1).** `searchexec.Contains(term, exprs...)` is the framework's only substring rule. The search's full text, every text field and the base's `r.where.XContains` (after G2) call it.
   - The term is trimmed. A blank term gives the literal `TRUE`, never `nil`, because a nil expression panics when a caller composes it.
   - A term longer than `search.MaxTermLength` (1000 characters) is `search.ErrTermTooLong` (400).
   - The escape character is `!`: `!` becomes `!!`, `%` becomes `!%`, `_` becomes `!_`. The SQL text is the same in the three dialects' literal syntax and does not depend on `standard_conforming_strings`.
   - PostgreSQL uses `expr ILIKE $1 ESCAPE '!'`, which a plain trigram index serves. MySQL, MariaDB and SQLite use `LOWER(expr) LIKE LOWER(?) ESCAPE '!'`, which does not depend on a `_bin` or `_cs` collation or on `PRAGMA case_sensitive_like`.
   - Each match is one atomic expression. Several matches are combined with `jet.OR`, so `scope AND (e1 OR e2)` never renders as `scope AND e1 OR e2`.
   - Folding and matching still follow the dialect and the column's collation. SQLite's `LOWER` folds ASCII letters only (unless the product loads ICU), so `'é'` does not find `'É'`. MySQL and MariaDB accent-insensitive collations (`utf8mb4_0900_ai_ci`, `uca1400_ai_ci`, `general_ci`) match the term `e` with `'é'`. The R3 tests pin both differences.

5. **One fork change for dialect behaviour (S3.1).** No package outside `db/internal/jet` can branch on the dialect when a statement serializes. R2 adds one constructor, `jet.DialectSwitch(func(Dialect) Expression)` and its typed forms, and makes `WriteAlias` escape the quote character. `db/UPSTREAM.md` lists both. `db/keyset` and `db/searchexec` use the switch for `ILIKE` against `LOWER() LIKE`, for the seek shape, and for literals per dialect.

6. **The pager has one contract on every dialect (S3.2).** The same rows, every walk complete (no row skipped, no row repeated), and the same cursor tokens. Order and matching follow the column's collation.
   - NULLs are last when ascending and first when descending, unless a key asks for `NullsFirst` or `NullsLast`. A dialect gets an explicit NULL placement only for a nullable key whose contract differs from the dialect default, so a NOT NULL `(created_at, id)` walk keeps its index.
   - MySQL gets the expanded seek (`a > ? OR (a = ? AND b > ?)`), because its optimizer does not range-scan a row-constructor inequality. PostgreSQL and SQLite keep the row value when every key is NOT NULL and has one direction.
   - Every codec refuses a cursor value that the column cannot hold with `ErrInvalidCursor` (400), never a database error. `TimeKey` and `InetKey` are PostgreSQL only; another dialect gets `keyset.ErrDialect`.
   - The cursor signature names columns, not SQL, and has one rendering. A cursor that PostgreSQL issues today on NOT NULL keys keeps its token. A nullable key, read as `""` today, changes its tokens once (see Consequences).
   - One cross-dialect contract test (S3.5) walks the same 200 rows with the same 40 searches: on binary collations it asserts one order and identical tokens; on each dialect's default collation it asserts complete walks.

7. **The generator emits PostgreSQL searches only in this part (S3).** A `nanorepo.Search` on another dialect is a gen error, as for the repository base ([ADR 0026](0026-nano-gen-repo.md), decision 7). The runtime does not wait for the generator.

8. **Tenant coverage is structural and instance-based (S5, S6).** The generator computes the read set of a search as a set of table instances (a table plus its alias), with each instance's join kind and `ON` clause. It then proves that each instance is covered. The tenant columns come from `Postgres.Scopes` in `nanogen/project.go`. Coverage is a fixpoint over five rules, which read only top-level `AND` conjuncts:
   1. **Direct.** `Scope`, `ScopeOrNull`, `ScopeNull` or the registered `ScopeFunc` names the column. A direct scope is a `WHERE` condition, so it is refused on the nullable side of a `LEFT JOIN`.
   2. **Equated.** A conjunct of the instance's `ON` clause (or correlated `WHERE`) equates the column with a covered tenant column, a field of the scope value, or, on a covered instance, the column that this column's own declared foreign key references (`configs.product_id = products.id`, with the key `configs.product_id → products.id`).
   3. **Referenced row.** A conjunct of the instance's `ON` clause matches a declared foreign key from a covered instance to this instance's key (`product_roles.id = memberships.role_id`). Then every tenant column of the instance is covered.
   4. **No tenant column.** The instance has no tenant column, and a conjunct of its `ON` clause (or correlated `WHERE`) matches a declared foreign key, in either direction, between it and a covered instance.
   5. **Root.** The searched table is covered directly, or through rules 2 to 4 over `INNER JOIN`s only.

   A conjunct under an `OR` counts for nothing. A wrong `ON` clause (`INNER_JOIN(webhooks, Bool(true))`, `webhooks.ID.EQ(webhook_requests.ID)`) matches no declared key, so it covers nothing. A search is accepted only when every instance is covered and every tenant column of every instance is covered or exempted (decision 11). Else the gen error (E9, E10) names the first uncovered column and its instance.

   `CROSS`, `FULL` and `RIGHT` joins are refused. A `From` or expression function may call only the go-jet constructors on an allowlist; raw SQL, `CustomExpression` and a package variable of a table type are refused, and a call the check cannot resolve fails closed.

9. **Hard scopes and context scopes (S6.1).** A column of `Scopes` that is not in `ContextScopes` is a hard scope: its value comes only from the typed scope value that the caller passes, `Resolve` refuses a zero field with `search.ErrUnscoped`, and a `ScopeFunc` on it is a gen error (E9). G6 adds `ContextScopes` to `core/nano/project`; each of its columns must also be in `Scopes`, else it is a project error. A context-scope column is scoped only by the function that the project registers for it. That function may return `TRUE`; a `nil` result is `ErrUnscoped`. So a context scope bounds the rows only when the context carries a value. On an instance that also has a hard tenant column, that column bounds them in every case.

10. **The runtime does not trust a query (S10).** `Resolve`, `Run`, `RunPage` and `transactor.Search` take the generated spec and compare pointers. `Spec` has unexported fields, behind a validating `DefineSpec`. In search mode, `keyset.Page` refuses a nil `Where`, so an empty scope list never runs as "select every row". A hand-written method gets an opaque `Plan`, which can narrow the condition and never widen it.

11. **Every exemption is visible (S6.6).** `nanorepo.Unscoped(column, "reason")` exempts one tenant column, and only when another hard tenant column of the same instance is covered (E30). So an `Unscoped` exemption never leaves an instance with no covered hard tenant column. `nano gen repo` writes `nanogen/search-exemptions.txt` with every `Unscoped`, `ScopeOrNull`, `ScopeNull`, rule-3 coverage, `IgnoresFullText` and `APIHandWritten`, and `nano gen -check` fails when the committed file is stale.

12. **Column types and foreign keys come from `nano gen db` (S7).** G1 adds two methods to each generated table type, next to `UniqueKeys()`: `ColumnTypes()`, the catalogue type name of each column, and `ForeignKeys()`, the declared foreign keys. `nano gen repo` reads both from Go source, so the repo step still needs no database. G1 waits for PR 8 of ADR 0026; the `nano gen db` of today writes neither method.
    - A column of an instance on the nullable side of a `LEFT JOIN` is nullable in the result, so a sort on it gets the nullable key form.
    - The tiebreak defaults to the primary key, in index order. An explicit `Tiebreak` must contain every column of one key of `UniqueKeys()`.

13. **`db/search` replaces `pkg/search` in steps (S11).**
    1. R1: `db/search` is new. `pkg/search` keeps its types and shares only the error sentinels, and forwards `IsClientError`, `EncodeCursor` and `DecodeCursor`. So `errors.Is` and the error envelope agree across both packages.
    2. G14: `Request[F, S]` moves to `db/search` for the searches that have no SQL builder (S14). `pkg/search` becomes generic type aliases of `db/search`. The framework's own importers move, and the transactor's field-by-field bridge goes away.
    3. Products move one search at a time, and change `x-go-type-import` in their OpenAPI spec.
    4. F1 deletes `pkg/search` when a code search over every repository that requires the framework finds no import.

    `pkg/search` gets no `Deprecated:` marker. The framework lint runs staticcheck with `checks: all`, so SA1019 would fail the lint of every product on the next framework bump (99 files in echopoint, 111 in anchor, 56 in atelier). Its package comment says that it holds aliases and will be removed.

## Alternatives

- **`db/search` holds the contract and the executor.** The executor imports the dialect packages, so every domain package would depend on SQL.
- **One package `db/searchexec` with the keys and the pager inside.** Hand-written searches and the transactor use the pager without a generated builder, and the pager is the concept of ADR 0001.
- **Keep the pager in `pkg/db/transactor` and generate code that imports it.** The search runtime would live in the root module, and the db module could never test it on three dialects.
- **Move the context transaction into the db module now.** That is the work of `db/exec` (stage 2). Doing it now pulls the stage forward with no benefit for search.
- **A copy of the pager in the search runtime.** A copy drifts; the prototype that copied it lost the `limit <= 0` guard.
- **PostgreSQL's default `\` escape with no `ESCAPE` clause.** It does not work on SQLite, and it depends on `standard_conforming_strings`.
- **Lower-case the term in Go.** Go and the database disagree on some letters (Turkish dotted I, final sigma). The term must fold by the same rule as the column.
- **Each dialect's own NULL default.** MySQL and SQLite put NULLs first when ascending, so one search would return another order on another database.
- **`OperatorSerializeOverride` instead of a dialect switch.** It covers operators only, not literals or seek shapes.
- **A direct scope on every tenant column of every table read.** It gives a child and a parent column of one name two scope fields, it drops the unmatched rows of a `LEFT JOIN`, and it never reads an `ON` clause, so a wrong join passes.
- **A search is scoped when any table it reads has a scoped tenant column** (the prototype). It accepts a cross join and a wrong `ON` clause.
- **Refuse `Unscoped` on every search bound to the API.** anchor's product-level endpoint reads every organization of one product on purpose. The same-instance rule blocks a search that is unscoped as a whole.
- **Typed column kinds in the fork (`ColumnInet`, `ColumnJSON`).** A kind that embeds `StringExpression` gives no compile-time safety, and one that does not breaks the expression algebra. The kinds also differ per dialect.
- **A schema manifest written from a live database.** It needs a database in the repo step and a second file to keep in sync; `nanogen.sum` already covers the table files.
- **A `Deprecated:` marker on `pkg/search`.** It fails the lint of every product on the next bump, which also blocks unrelated security patches. The end state is deletion, so a marker adds only CI breakage.
- **Each product defines its own request type for searches with no SQL builder.** It copies one shape into three products, and the copies drift.

## Consequences

- The runtime lane (R1 to R12) touches only the db module and the root module's `pkg/`. The generator lane (G1 to G14) starts after PR 8 of ADR 0026 merges. It touches `gen/`, plus `db/generator` (G1), `core/nano/project` (G6), and `pkg/search` and the docs (G14).
- `pkg/db/transactor` keeps the context transaction and the row-lock rules. `transactor.Page(...).Run` and every search refuse a requested row lock with `ErrRowLockSearch`.
- Behaviour changes on every endpoint that adopts a generated search:
  - `full_text_search` ignores case (ASCII letters only on SQLite), matches `%`, `_` and `\` literally, trims the term, and ignores a blank term.
  - More than 1000 values in one list filter (`search.MaxInValues`), or a term longer than 1000 characters, is 400.
  - A nullable sort key puts NULLs last when ascending, so the cursor tokens of such a sort change once.
  - `In()` with no values from Go matches no row; the API mapper still treats an empty list as no filter.
  - A Go caller that passes a limit of 0, a negative limit, a limit over 1000 or a negative offset gets an error.
- Each product PR follows the gate steps of the cross-repo rollout rules: export the product's gate flows, grep every property the PR changes (`full_text_search`, the range bounds, the sort cursors, the new limits), update each flow that asserts the old result, and publish it where a published version exists.
- Each product commits `nanogen/search-exemptions.txt`, and reviews each exemption in the diff.
- The CHANGELOG says: "The db module now holds the search contract and the keyset pager." It names no other library.
- Two decisions wait for Alexis's confirmation, and the design picks the option shown until then: the tightened `Unscoped` rule of decision 11 (approved answer 7), and rule 3 of decision 8, which trusts referential data. If rule 3 is dropped, a join that it covers needs `Unscoped` or a tenant equality in the `ON` clause.
