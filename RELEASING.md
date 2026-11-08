# Releasing

This repository releases four Go modules. This runbook gives the rules for their `go.mod` files, the release steps with exact commands, the verification, the rollback, and the upgrade notes for consumers. The design is in [docs/design/beta-readiness.md](docs/design/beta-readiness.md), section D.

## Modules and tags

| Module | Directory | Tag | First beta |
|---|---|---|---|
| `github.com/nanostack-dev/nanostack-framework/pkg/functional` | `pkg/functional` | `pkg/functional/vX.Y.Z` | `pkg/functional/v0.23.0-beta.1` |
| `github.com/nanostack-dev/nanostack-framework/db` | `db` | `db/vX.Y.Z` | `db/v0.23.0-beta.1` |
| `github.com/nanostack-dev/nanostack-framework` | `.` | `vX.Y.Z` | `v0.23.0-beta.1` |
| `github.com/nanostack-dev/nanostack-framework/gen` | `gen` | `gen/vX.Y.Z` | the first release after `v0.23.0-beta.1` |

- Each module has its own version ([ADR 0014](docs/adr/0014-one-repository-with-nested-modules.md)). The root `go.mod` records the nested versions that it was tested with. Do not tag a module that did not change.
- `db` requires `pkg/functional`. The root requires `db` and `pkg/functional`. `gen` requires the root, and through it `db` and `pkg/functional`. So the tag order is always leaves first: `pkg/functional`, then `db`, then the root, then `gen`.
- `gen` holds the generators and the `nano` CLI (`gen/cmd/nano`). The root never requires `gen`, so `golang.org/x/tools` and the other generator dependencies stay out of the module graph of a runtime consumer. A service pins `nano` with a `tool` directive ([Consumers of nano](#consumers-of-nano)).
- `db/tests` and `db/examples` are never tagged. They keep their local `replace` lines.
- The module paths have no `/vN` suffix, so the major version stays 0 or 1.
- A pre-release suffix (`-beta.N`) keeps `go get ...@latest` and `go get -u` on the last release. Use it while the API can still change.

## go.mod rules

`main` is always in the **development form**. A release commit, which only a tag points to, is in the **release form**.

1. Development form (`main` and every branch): a released module requires each other module of this repository at the placeholder `v0.0.0`, with a `replace` to its relative directory (`./db`, `../pkg/functional`, `../`). The two always go together. A dependency's `replace` does not apply in the module that requires it, so `gen` also repeats the root's two `replace` lines: the root requires `db` and `pkg/functional` at `v0.0.0`. So `GOWORK=off` builds and CI test the modules of one commit together, and one pull request can change `db` and the root at once.
2. Release form (a tagged commit): no `replace`, no `v0.0.0` and no pseudo-version. A released module requires each other module of this repository at its tag. Consumers ignore `replace`, and `go install module@version` (for example of `gen/cmd/nano`) refuses a `go.mod` that has one.
3. A release commit is never merged into `main`. It lives on a `release/vX.Y.Z` branch, and `main` keeps the development form.
4. `go.work` and `go.work.sum` are for development only. They are git-ignored. CI sets `GOWORK=off` and uses the `replace` lines.
5. `lint/cmd/lintconfig` stays in the root module at its path, because consumers pin it as a `tool`.

`go mod tidy` works in the development form: run it with `GOWORK=off` in the module that changed.

### The release check

`.github/workflows/release.yml` runs on every pull request, on each pushed release tag, and on `workflow_dispatch` with a tag:

| Trigger | What it checks |
|---|---|
| Pull request | The development form: an intra-repository `v0.0.0` requirement has a `replace` to a relative directory and the reverse (error). Any other `replace` is an error. No `go.work` is committed (error). |
| `workflow_dispatch`, tag does not exist | A dry run before you tag: the release rules for the module of that tag, on the branch you select. |
| Pushed tag, or `workflow_dispatch` with an existing tag | The release form at the tagged commit: no `replace`, no `v0.0.0`, no pseudo-version, and each required intra-repository version is a tag in the history of this tag (leaves first). Then a scratch module runs `go get` of the tag, with this repository read from git (`GONOPROXY`) and not from the proxy cache, and `go build` and `go vet` of packages from each module. |

## Release steps

A release is a chain of at most three commits on a `release/vX.Y.Z` branch, cut from `origin/main`. The tags go leaves first: `pkg/functional`, then `db`, then the root, then `gen`. Tag only the modules that changed; a module that did not change keeps its last tag, and the others require that tag. The example is the first beta.

Before you start:

```sh
git fetch origin --tags
git switch -c release/v0.23.0-beta.1 origin/main
git tag -l 'v*' 'db/v*' 'pkg/functional/v*' 'gen/v*' --sort=-v:refname | head
```

Make sure that the new tags do not exist. A tag is never moved or deleted (see [Rollback](#rollback)). Make sure that CI passed on the `origin/main` commit.

**Wait for a tag on the remote, not in the local cache.** `go list -m` and `go mod tidy` read the local module cache first. A cache entry for a version that no tag produced (for example from a simulated release with a local `GOPROXY`) satisfies them before the tag exists, and `go mod tidy` then writes the hash of that local copy to `go.sum`. `GOPRIVATE` covers this repository, so the checksum database does not catch it, and CI fails later with a checksum mismatch. So:

- Check that a tag exists with `git ls-remote --exit-code --tags origin refs/tags/<tag>`.
- Run each `go list -m` and each `go mod tidy` of the release steps with an empty module cache: `GOMODCACHE="$(mktemp -d)"`.
- `GOPROXY=direct` and `GOPRIVATE` make Go fetch this repository with git over HTTPS. A git `url.<ssh>.insteadOf https://github.com/nanostack-dev` rule sends those fetches to SSH; when SSH is not available, run the Go commands with `GIT_CONFIG_GLOBAL` pointing at a config without that rule (the repository is public).

### 1. pkg/functional

`pkg/functional` requires nothing from this repository, so its tag goes on the `origin/main` commit itself:

```sh
gh workflow run release.yml --ref main -f version=pkg/functional/v0.23.0-beta.1   # dry run
git tag -a pkg/functional/v0.23.0-beta.1 -m "pkg/functional v0.23.0-beta.1" HEAD
git push origin pkg/functional/v0.23.0-beta.1
git ls-remote --exit-code --tags origin refs/tags/pkg/functional/v0.23.0-beta.1
GOWORK=off GOPROXY=direct GOMODCACHE="$(mktemp -d)" \
  go list -m github.com/nanostack-dev/nanostack-framework/pkg/functional@v0.23.0-beta.1
```

### 2. db

One commit on the release branch puts `db/go.mod` in the release form. `go mod tidy` joins go-jet's two `require` blocks: before you commit, move `go-cmp` and `testify` back under `// used in tests`, so that the diff is only the `pkg/functional` line, the dropped `replace` and `go.sum` ([db/UPSTREAM.md](db/UPSTREAM.md), Dependency row):

```sh
(cd db &&
  go mod edit \
    -dropreplace github.com/nanostack-dev/nanostack-framework/pkg/functional \
    -require github.com/nanostack-dev/nanostack-framework/pkg/functional@v0.23.0-beta.1 &&
  GOWORK=off GOMODCACHE="$(mktemp -d)" go mod tidy &&
  GOWORK=off go build ./... && GOWORK=off go test -race -count=1 ./...)
git diff db/go.mod   # restore the two require blocks if tidy joined them
git commit -am "release: db v0.23.0-beta.1"
git push origin release/v0.23.0-beta.1
gh workflow run release.yml --ref release/v0.23.0-beta.1 -f version=db/v0.23.0-beta.1   # dry run
git tag -a db/v0.23.0-beta.1 -m "db v0.23.0-beta.1" HEAD
git push origin db/v0.23.0-beta.1
git ls-remote --exit-code --tags origin refs/tags/db/v0.23.0-beta.1
GOWORK=off GOPROXY=direct GOMODCACHE="$(mktemp -d)" \
  go list -m github.com/nanostack-dev/nanostack-framework/db@v0.23.0-beta.1
```

### 3. Root

One more commit puts the root `go.mod` in the release form. `go mod tidy` completes `go.sum`, and it can raise requirements, because the tagged modules add their own requirements to the graph. Review the `go.mod` diff before you commit:

```sh
go mod edit \
  -dropreplace github.com/nanostack-dev/nanostack-framework/db \
  -dropreplace github.com/nanostack-dev/nanostack-framework/pkg/functional \
  -require github.com/nanostack-dev/nanostack-framework/db@v0.23.0-beta.1 \
  -require github.com/nanostack-dev/nanostack-framework/pkg/functional@v0.23.0-beta.1
GOWORK=off GOMODCACHE="$(mktemp -d)" go mod tidy
git diff go.mod
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...
git commit -am "release: v0.23.0-beta.1"
git push origin release/v0.23.0-beta.1
gh workflow run release.yml --ref release/v0.23.0-beta.1 -f version=v0.23.0-beta.1   # dry run
git tag -a v0.23.0-beta.1 -m "v0.23.0-beta.1: <one-line summary>" HEAD
git push origin v0.23.0-beta.1
gh release create v0.23.0-beta.1 --prerelease --verify-tag --notes-file <notes>
```

The CHANGELOG.md entries land on `main` before the release, in the pull requests that make the changes.

### 4. gen

One more commit puts `gen/go.mod` in the release form. It drops the three `replace` lines and requires the root tag. `go mod tidy` then requires `db` and `pkg/functional` at their tags where `gen` needs them. Review the `go.mod` diff before you commit. `gen` must require a root tag that has no `gen/` directory, the root tag of step 3. The real `v0.23.0-beta.1` root tag and older ones still hold the `gen/...` packages, and a consumer that resolves both modules fails with `ambiguous import`; the consumer build of `release.yml` fails the same way. So `gen`'s first tag comes with the first release after `v0.23.0-beta.1`. The example keeps the version of the other steps:

```sh
(cd gen &&
  go mod edit \
    -dropreplace github.com/nanostack-dev/nanostack-framework \
    -dropreplace github.com/nanostack-dev/nanostack-framework/db \
    -dropreplace github.com/nanostack-dev/nanostack-framework/pkg/functional \
    -require github.com/nanostack-dev/nanostack-framework@v0.23.0-beta.1 &&
  GOWORK=off GOMODCACHE="$(mktemp -d)" go mod tidy &&
  GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test ./...)
git diff gen/go.mod
git commit -am "release: gen v0.23.0-beta.1"
git push origin release/v0.23.0-beta.1
gh workflow run release.yml --ref release/v0.23.0-beta.1 -f version=gen/v0.23.0-beta.1   # dry run
git tag -a gen/v0.23.0-beta.1 -m "gen v0.23.0-beta.1" HEAD
git push origin gen/v0.23.0-beta.1
git ls-remote --exit-code --tags origin refs/tags/gen/v0.23.0-beta.1
```

`release.yml` builds a scratch consumer that pins `gen/cmd/nano` with `go get -tool` and runs `go tool nano`, as a service does.

### 5. Verify

- The push of each tag starts `release.yml`. Make sure that the run for each tag passed: `gh run list --workflow release.yml --limit 5`. To run the verification again: `gh workflow run release.yml --ref main -f version=v0.23.0-beta.1`. Each tag has its own concurrency group, so you can dispatch more than one tag at a time.
- Do the consumer build check of [docs/runbooks/deployment.md](docs/runbooks/deployment.md) in echopoint and anchor with the published tag, in a scratch copy or a branch. Before the tags exist, that check must replace every released module that the service resolves (step 1 of that runbook): a replace of the root module alone leaves `db` and `pkg/functional` at versions that are not tagged, and a service that pins `nano` also resolves `gen`.

## Consumers of nano

A service runs the generators through a `tool` directive in its own `go.mod`. The directive pins the `gen` module, and `go.sum` records it:

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>
go tool nano gen
```

`go tool nano gen` runs every step in order, and CI runs it with `-check` ([generation](docs/generation.md#the-pipeline-nano-gen)). The pipeline rejects a `//go:generate` line that runs `nano gen`. The tool's dependencies (`golang.org/x/tools` among them) join the service's module graph, not the graph of a module that only imports the framework's runtime packages. The framework's own root module cannot pin `nano` this way: `gen` requires the root, so a `tool` line in the root `go.mod` would bring `gen` and its dependencies back into the root graph. It runs `go -C <path to gen> run ./cmd/nano gen -dir <directory>` instead.

## Rollback

- Never move or delete a pushed tag. The Go module proxy and the checksum database keep the first content of a version. A moved tag gives a checksum error to every consumer that already has the old content.
- To replace a bad version, publish the next pre-release or patch (for example `db/v0.23.0-beta.2`). Then require it in the release commits of the modules that depend on it, and tag those again, leaves first.
- Add a `retract` directive for the bad version to the `go.mod` of the module in the next release, with a comment that gives the reason:

  ```
  retract v0.23.0-beta.1 // go.sum hash mismatch in db; use v0.23.0-beta.2
  ```

- A consumer goes back to the last good version with `go get <module>@<version>`, then `go mod tidy`.

## Upgrade from v0.23.0-beta.1

`cmd/nano` moved to the `gen` module, at `gen/cmd/nano`. The root module no longer contains the generators, so `go run github.com/nanostack-dev/nanostack-framework/cmd/nano` fails. Pin the tool once and change each `//go:generate` line:

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/gen/cmd/nano@<gen tag>
grep -rl 'nanostack-framework/cmd/nano' --include='*.go' . \
  | xargs sed -i '' 's#go run github.com/nanostack-dev/nanostack-framework/cmd/nano#go tool nano#g'
go mod tidy
```

(On Linux, use `sed -i` without `''`.) A Go file that imports a `gen/...` package (a `genconfig.Run` or `wire.Run` test) resolves it through the `tool` requirement. The generated files do not change.

## Upgrade from v0.22.0

This is the minimum upgrade. fx modules, zerolog and the legacy HTTP stack keep working.

1. Upgrade the root module. `go mod tidy` adds `pkg/functional` and `db`:

   ```sh
   go get github.com/nanostack-dev/nanostack-framework@v0.23.0-beta.1
   go mod tidy
   ```

   Upgrade the root and `pkg/functional` together. A module graph that keeps root v0.22.0 (which contains `pkg/functional`) next to the `pkg/functional` module fails with "ambiguous import". The `pkg/functional` import path does not change.
2. Change every go-jet import to Nanostack DB. `pkg/db/transactor` and `pkg/jetx` now take Nanostack DB types, and go-jet types do not satisfy them:

   ```sh
   grep -rl 'github.com/go-jet/jet/v2' --include='*.go' . \
     | xargs sed -i '' 's#github.com/go-jet/jet/v2/#github.com/nanostack-dev/nanostack-framework/db/#g'
   go mod tidy   # drops github.com/go-jet/jet/v2
   ```

   (On Linux, use `sed -i` without `''`.) The paths `pkg/db/transactor`, `pkg/jetx`, `pkg/search` and `pkg/db/pgerr` do not change.
3. Change the model generator (`dbgen.go`) to import `github.com/nanostack-dev/nanostack-framework/db/generator/postgres` and `.../db/generator/template`. Regenerate the models and review the diff. A model changes in more than its imports only where a template renames a field: the generator then adds a `column:"<name>"` tag.
4. Review the changes that can change SQL or values (go-jet v2.15.0 to v2.16.1 + #628, and the fork):
   - `Raw` with named arguments where one name is a prefix of another (`:id`, `:id_2`) now binds the correct value.
   - A type wrapper such as `IntExp(expr)` no longer changes `expr`.
   - A `functional.Option` field in a scan destination is filled by qrm like a `*T` field. A `functional.Option` value in `MODEL`, `VALUES`, `SET`, `Literal` or `Raw` is NULL when it is absent. None in `MODEL` gives NULL, not DEFAULT. Search the models and `QRM` destinations for `functional.Option` first.
   - `FOR(locks ...RowLock)` is variadic. Call sites compile unchanged. A type that implements `SelectStatement` itself must change.
   The full list is in [docs/design/beta-readiness.md](docs/design/beta-readiness.md), section A5, and in [db/UPSTREAM.md](db/UPSTREAM.md).
5. Silence the deprecations until the service migrates. The legacy packages (`modules/...` except `modules/nanofx`, `app`, `pkg/fxlog`, `pkg/httputil/requestlog`, and `modules/httpserver.StrictErrorHandler`) now carry a `Deprecated:` paragraph, so staticcheck reports SA1019 at every import. Add an SA1019 exclusion scoped to those import paths, for example a rule on the text `is deprecated: removed with fx (ADR 0013)`; where the shared lint config takes one rule per file, add `staticcheck` to the rule of each importing file and raise the exclusion budget in the same reviewed change. Remove the rules as the service moves each module.
6. Check the behaviour changes of the beta audit fixes that reach a v0.22.0 service ([CHANGELOG.md](CHANGELOG.md), Security and Changed):
   - `modules/apisec` now matches requirements as chi does (`apisec.MatchChi()`). A test or a service that calls `apisec.NewResolver(doc)` itself on a chi router passes `apisec.MatchChi()`; without it the resolver matches as `http.ServeMux` and fails on paths that conflict there. Refuse a request the resolver does not match.
   - `pkg/httputil/errorenvelope` and `StrictErrorHandler` write a stream error frame without a second status once the stream started (behind `core/log.HTTPMiddleware` only).
7. Run the repository and integration suites. Echopoint's `cmd/it/repository` suite runs in no CI check, so run it by hand.

Each later step is a separate service pull request, with the golden HTTP suite before and after: logging with `LOG_WIRE=legacy`, typed config, and the move of each module to nano. When a service moves to typed config, it renames the keys of its committed YAML to the `config:` names (`dbname` becomes `db`, `basePath` becomes `base_path`): the `yaml:` tags of the nano `Config` types serve only the deprecated fx loader, `core/config` does not expand `${VAR}`, and it rejects a secret in a file layer (see [docs/modules.md](docs/modules.md#differences-from-the-fx-modules)). The HTTP error path keeps the legacy envelope through `pkg/httputil/errorenvelope`: a service on `core/log` passes `errorenvelope.New(...).RequestError` and `ResponseError` to its strict server (chi or nano), and the router's `NotFound` and `MethodNotAllowed` handlers from the same `Writer`. Before it adopts generated validation, it updates the gate assertions on kin-openapi message text (design C3). Its PR lists the router differences from chi in [docs/http-boundary.md](docs/http-boundary.md#routing). When a service moves to `nano.Main`, set `stop_grace_period` in its stack to at least `StopTimeout + StopGrace × Stoppers + 5s` (30s for echopoint and anchor with the 15s default). Docker's default is 10s. `APP_DRAIN_DELAY` is counted inside `StopTimeout`: when the service sets it (about 10s behind Traefik), raise `APP_STOP_TIMEOUT` by the same amount and keep `shutdown_timeout` below `StopTimeout − DrainDelay`. Start fails when `DrainDelay` leaves the Runners less than `StopGrace` (1s) of `StopTimeout`. A service on fx that moves a module whose constructor dials (`nanopg.NewDB`, `nanocache.NewStore`, `nanolock.NewClient`) provides the context with `nanofx.Context(signalCtx)`; one that moves tracing registers both `nanofx.Lifecycle[*nanotrace.Provider]` and `nanofx.Lifecycle[*nanotrace.Installer]`. A `Config` built by hand, in a test or an `Override`, starts from the module's `DefaultConfig()`. A service that moves its cache to `redis/nanocache` starts cold for the keys whose parts hold `:` or `\`, which `nanocache` escapes; the other keys do not change. Long-lived handlers (SSE) select on `nanoserver.Draining(r.Context())` to end their stream when the drain starts.

Anchor pins v0.20.0. Read the v0.21 and v0.22 notes first.
