# Testing

Run baseline checks from the module root:

```sh
go test -race -count=1 ./...
go vet ./...
go build ./...
```

Select affected packages for iteration, such as `go test -race -count=1 ./pkg/functional ./pkg/db/transactor`. Database tests use the `integration` build tag and skip if their DSN is absent; skipped tests are not database verification.

## PostgreSQL integration

Use a disposable PostgreSQL server. The transactor tests expect `PGERR_TEST_DSN`; the traced-driver test in `modules/postgres` expects `POSTGRES_TEST_DSN`; migrations tests expect `MIGRATIONS_TEST_DSN` with permission to create and remove temporary databases.

```sh
docker run --rm -d --name framework-docs-postgres -p 55432:5432 \
  -e POSTGRES_PASSWORD=itpass -e POSTGRES_DB=pgerrit postgres:16
PGERR_TEST_DSN='postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable' \
  go test -tags=integration -race -count=1 ./pkg/db/transactor
MIGRATIONS_TEST_DSN='postgres://postgres:itpass@localhost:55432/postgres?sslmode=disable' \
  go test -tags=integration -race -count=1 ./modules/migrations
POSTGRES_TEST_DSN='postgres://postgres:itpass@localhost:55432/postgres?sslmode=disable' \
  go test -tags=integration -race -count=1 ./modules/postgres
docker stop framework-docs-postgres
```

Wait until PostgreSQL accepts connections before running tests. See [transactor tests](../../pkg/db/transactor/result_integration_test.go) and [migration tests](../../modules/migrations/migrations_integration_test.go) for the fixture contract.

## Redis and Valkey integration

`pkg/redisx` and `pkg/testkit/redistest` use the `integration` tag too. They need no setup: `redistest` starts a Valkey container through Docker on first use and removes it when the test binary exits. Set `REDIS_TEST_ADDR` to use a server you run, or `REDIS_TEST_IMAGE` to test another server version.

```sh
go test -tags=integration -race -count=1 ./pkg/redisx ./pkg/testkit/redistest
REDIS_TEST_IMAGE=redis:7-alpine go test -tags=integration -race -count=1 ./pkg/redisx
```

Failure tests use `redistest.StartServer`, which always starts a container of its own so that a restart or a pause does not disturb the other tests. With `REDIS_TEST_ADDR` set and no Docker, those tests are skipped; skipped tests are not failure verification. `redistest.FaultyClient` loses command replies on purpose and works with any server.

## Generated APIs and consumers

Regenerate functional families with `go generate ./pkg/functional`, then review the generated diff and rerun that package's tests. Keep application-generated files in their owning repository.

The current framework tree has no tracked CI or golangci-lint configuration. `gorules/` is consumed by application lint configuration. Do not claim a missing framework CI gate passed. Public changes also need affected consumer lint, integration/contract checks and builds.

Documentation-only changes use local link validation and `git diff --check`; do not add runtime tests merely to mirror prose.
