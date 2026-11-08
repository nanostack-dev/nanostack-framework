# 0020: Framework-owned db/migrate replaces golang-migrate

- Status: proposed
- Date: 2026-11-07

## Context

`modules/migrations` runs `golang-migrate/migrate/v4` with its PostgreSQL driver and a `file://` source. golang-migrate stores one row: the current version and a dirty flag. The module reads that row and stops on a dirty state (`handleDirtyState`).

That record is not enough:

- No checksums. Editing a migration that has already run is not detected, so two databases at the same version can have different schemas.
- No drift detection between the files and what ran.
- No lint for statements that lose data or take long locks.
- A failed migration leaves only a dirty flag, with no record of which statement failed.

The framework also needs to run migrations outside service startup: inside `nano gen`, to generate code from a migrated schema, and inside the test module, to build template databases. Both need a library API, not a CLI.

## Decision

The framework owns a `db/migrate` package.

- Migrations are versioned SQL files embedded with `embed.FS`.
- A history table records, for each migration, the version, name, SHA-256 checksum of the file, start and end time, and outcome.
- Before running, `db/migrate` compares every applied file with its recorded checksum and refuses to continue on a difference (drift), naming the file. A repair is an explicit command.
- A run holds a lock so that two instances never migrate at once (an advisory lock on PostgreSQL).
- Each migration runs in a transaction where the dialect allows it. A statement that cannot run in a transaction, such as `CREATE INDEX CONCURRENTLY`, is declared in the file.
- A lint step flags destructive and long-locking statements (dropping a table or column, changing a column type, a non-concurrent index on a large table). A flagged statement needs an explicit annotation in the file.
- One API serves service startup, `nano gen` and the test module's template databases.
- The target is the three dialects of the `db` module ([ADR 0015](0015-fork-go-jet-as-the-db-module.md)): PostgreSQL, MySQL/MariaDB and SQLite. Dialect-specific code (lock, history table DDL, transaction rules) sits behind a small interface. PostgreSQL ships first; MySQL/MariaDB and SQLite follow before the test module builds template databases for them.
- `db/migrate` executes through its own minimal connection interface, not through the query builder, so a migration run needs no generated code.
- On first run against a database that golang-migrate managed, `db/migrate` reads the old version row, refuses a dirty one, and records checksums for the files at or below that version.

## Alternatives

- **Keep golang-migrate.** Rejected for the missing checksums, drift detection and lint above.
- **goose.** A library API and Go-function migrations, but its history table has no checksums either.
- **Atlas.** Strong drift detection and lint, but it brings its own schema language and binary toolchain, and several features depend on its cloud service.
- **dbmate, tern.** Small and CLI-first. dbmate has no checksums, and tern supports only PostgreSQL, against a three-dialect target.
- **Flyway, Liquibase.** Checksums and drift detection, on a JVM toolchain the Go services do not otherwise need.

## Consequences

- The framework owns migration code, per-dialect tests and the lint rules.
- An edited applied migration fails the next deploy with the file name instead of silently diverging.
- Products move their migration files under `db/migrate` and keep their version numbers. The import step adopts their golang-migrate history once.
- `modules/migrations` and the golang-migrate dependency are removed after products move.
- Whether `db/migrate` supports down migrations is decided in its design. The [rollback runbook](../runbooks/rollback.md) already prefers a reviewed forward fix to undoing database changes.
