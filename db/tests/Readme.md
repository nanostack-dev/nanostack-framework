# Integration tests

These suites test the generator, the statement builders and query result mapping against
real databases. They come from go-jet; Nanostack replaced how they are set up (docker-compose,
Makefile, git submodule and `make jet-gen-all`) with the `internal/testenv` harness.

## Run

Docker must be running. From `db/tests`:

```sh
go run ./tools/fetchtestdata     # once per checkout, and after the pin changes
go test -tags postgres ./...
```

The first command fetches the test data (see below) and links `db/tests/testdata` to it. It
must come first: the suites import the Go package `testdata/results/common`, which exists
only inside the test data, so they do not compile without the link. Then each suite's
`TestMain` calls the harness, which:

1. checks the test data again (and refetches a cache with local changes);
2. builds this repository's `jet` CLI into the run directory and puts it first on `PATH` (the
   generator suites run `jet`);
3. starts the databases with testcontainers, in parallel, and loads the data once;
4. regenerates the models from each database into the run directory;
5. runs the suite once per database, then stops the containers. A database whose models differ
   from the committed ones (CockroachDB, MariaDB) gets its own build of the suite (see
   Generated models).

`-tags postgres` compiles `postgres/range_test.go`, which upstream gates behind that tag
(range types). Without it those 4 tests are left out; everything else runs.

| Package | Databases | Image |
|---|---|---|
| `postgres` | PostgreSQL 14 (oldest supported), PostgreSQL 18, CockroachDB | `postgres:14.24`, `postgres:18.6`, `cockroachdb/cockroach:v26.3.5` |
| `mysql` | MySQL 8.4 LTS, MariaDB 11.4 LTS | `mysql:8.4.13`, `mariadb:11.4.13` |
| `sqlite` | SQLite (files, no container) | |

The images are pinned in `internal/testenv/source.go`.

The CockroachDB run of `postgres` also starts a PostgreSQL container (the newest selected
one, else 18), and the MariaDB run of `mysql` also starts MySQL: some generator tests read
`dbconfig.Pg*` or `dbconfig.MySQL*` whatever the source, as they did in upstream CI, where every
service was up.

## Options

| Variable | Effect |
|---|---|
| `NANODB_DIALECTS` | Comma-separated subset: `postgres` (14 and 18), `postgres14`, `postgres18`, `cockroach`, `mysql`, `mariadb`, `sqlite`, or `all` (default). A package with nothing selected reports `[no tests to run]`. A value that names no dialect (only commas or spaces) is an error, not an empty selection. |
| `NANODB_TESTDATA` | Use this jet-test-data checkout instead of the cache. |
| `NANODB_STMT_CACHE=true` | Run the suites with statement caching, 2 to 4 times in a row (upstream's `JET_TESTS_WITH_STMT_CACHE`). |

```sh
NANODB_DIALECTS=postgres18 go test -tags postgres ./postgres -run TestSelect   # one database, some tests
go test -short ./...                                                           # no containers: SQLite only
```

With `-short`, the `postgres` and `mysql` packages start nothing and report `[no tests to run]`,
with a `testenv: ... skipped` line saying why. SQLite needs no container and still runs.

## Test data

The SQL dumps, SQLite files and expected results live in
[go-jet/jet-test-data](https://github.com/go-jet/jet-test-data), pinned at commit
`7527e38f8290db3ef359eb1426ff49015528ccd0` (`TestDataCommit` in `internal/testenv/testdata.go`).
That repository has no license, so it is never committed here. On first use the harness runs
`git fetch --depth=1` of that commit into `<user cache dir>/nanostack-db/testdata/<sha>`
(`~/Library/Caches` on macOS, `~/.cache` on Linux) and reuses it afterwards while `HEAD` is the
pinned commit and `git status` shows no change to a tracked file; otherwise it fetches it
again. `db/tests/testdata` is a git-ignored symlink to it, because the suites read expected
results through `testdata/results`.

Each run works on copies of the SQLite files, so a suite that writes cannot change the shared
cache.

## Run directories

Each run gets a directory under `<user cache dir>/nanostack-db/run/`: the `jet` binary, the
generated models of each database, SQLite copies, and one working directory per database
(`root-<database>/<package>`). The suites run with that working directory, so the files the
generator suites write (`./.gentestdata2`, `./.tempTestDir`) never collide when two `go test`
processes run the same package. The directory is removed at the end; a run killed by a test
timeout or a panic leaves it, and the next run deletes it once its process is gone.

The same interruption skips the container cleanup, and Testcontainers' Ryuk reaper removes
the containers when the process exits. The harness refuses to start containers with Ryuk
disabled (`TESTCONTAINERS_RYUK_DISABLED=true`).

## Known failures

A few upstream tests fail against the newer database images for reasons outside the library
(`internal/testenv/knownfailures.go`, one row each in `db/UPSTREAM.md`). The harness runs the
suite without them, then runs each alone and requires it to fail with the recorded output.
A run where one passes, or fails differently, fails: update the entry.

## Generated models

The suites import models generated from the test databases (`.gentestdata/`). They are
committed, generated by this repository's generator with the options of upstream's init program:

- `jetdb/*`: PostgreSQL 18, schemas `northwind`, `dvds`, `test_sample`, `chinook`, `chinook2`;
- `mysql/*`: MySQL 8.4, databases `dvds`, `dvds2`, `test_sample`;
- `sqlite/*`: `sakila.db`, `chinook.db`, `test_sample.db`;
- `option/{jetdb,mysql,sqlite}/*` (Nanostack): Option-mode models (nullable columns as
  `functional.Option[T]`) of `test_sample` and `dvds` (`sakila.db` on SQLite), models only,
  the same as `jet -nullable=option -skip-sql-builder`. The Option suites (`option_test.go`,
  `query_test.go`) use them with the pointer-mode `table` packages.

`TestGeneratedModelsUpToDate` (one per package) compares them with the output of the generator
against the running database, so it fails when either the generator or the test data changes
without a refresh. To refresh:

```sh
go run ./tools/gentestdata
```

It generates into a temp dir and replaces a committed directory only when the generator wrote
files for every directory, so a broken database or cache never empties the tree.

PostgreSQL 14 produces the same models as 18, so its run uses the committed ones. CockroachDB
and MariaDB produce different ones (wider integer types, no range tables, no
`address.location`). Upstream regenerated the models before each of those runs and compiled
the suite against them. The harness does the same without touching the tree: it builds the
package again with `go test -c -overlay`, which replaces every committed model file with the
one generated from that database, and runs that binary against the same containers (the
parent passes their addresses). A suite that does not compile against those models fails the
run. The CockroachDB build leaves out `-tags postgres`, as upstream CI did, because the range
tests need tables CockroachDB does not have. `TestGeneratedModelsUpToDate` then checks that the
binary was compiled against its own database's models and logs how they differ from the
committed ones. To look at another database's output:
`go run ./tools/gentestdata -dialects cockroach -out /tmp/models`.

## Configuration seam

`dbconfig` keeps upstream's names, users, passwords and database names; the harness sets the
host and port of each run. PostgreSQL's port is fixed for the whole process (a local proxy
reserved when `dbconfig` initialises), because `postgres/generator_template_test.go` copies
`dbconfig.PgPort` into a package variable before `TestMain` runs.

The test files themselves are upstream's, unchanged except for the import path, so upstream
diffs stay readable. Only `main_test.go`, `dbconfig`, `internal/`, `tools/`, new files and
`mysql/lock_test.go` (a table lock leak; see `db/UPSTREAM.md`) differ.

## Random order

`go test -shuffle=on` runs the tests in random order and prints the seed; CI does it weekly.
Reproduce a failure with `-shuffle=<seed>`.
