# Lint standard

One golangci-lint configuration for every Nanostack Go service. The framework owns the base. A service owns only its additions, and commits the merged result so CI, editors and the golangci-lint GitHub Action read an ordinary `.golangci.yml`.

| File | Owner | Content |
|---|---|---|
| [`base.golangci.yml`](base.golangci.yml) | framework | 34 linters and their settings |
| [`golangci-lint-version`](golangci-lint-version) | framework | the golangci-lint release the base is written for |
| `.golangci.local.yml` | service | module path, build tags, ruleguard files, exclusions and their budget |
| `.golangci.yml`, `.golangci-lint-version` | generated | committed; never edited by hand |

golangci-lint has no `extends`, and its maintainers declined remote configuration ([#2137](https://github.com/golangci/golangci-lint/issues/2137)). The choice of linters and the alternatives are in the [research](../docs/research/golangci-lint-standard.md); open gaps in this design are in the [architecture review](../docs/research/lint-standard-architecture.md).

## Adopt it in a service

```sh
go get -tool github.com/nanostack-dev/nanostack-framework/lint/cmd/lintconfig
go tool lintconfig          # writes .golangci.yml and .golangci-lint-version
go tool lintconfig -check   # in CI: fails when the committed files are stale
```

When the lint configuration lives above the module, as in a repository whose module is in `apps/<name>`, pass the directory: `go tool lintconfig -dir ../..`.

The framework version in the service's `go.mod` decides which base it gets, so a lint change reaches a service only when it bumps the framework.

A minimal `.golangci.local.yml`:

```yaml
run:
  build-tags: [integration] # files behind a build tag are never linted without it
formatters:
  settings:
    goimports:
      local-prefixes: [echopoint]
linters:
  settings:
    gocritic:
      enabled-checks: [ruleguard]
      settings:
        ruleguard:
          rules: ${base-path}/gorules/functional.go,${base-path}/gorules/transactor.go
          failOn: all
```

## How the merge works

| Overlay value | Result |
|---|---|
| A key the base lacks | Added. |
| A mapping | Merged key by key. |
| A list | Gains the overlay items it does not already hold. Exclusion rules are appended. |
| A scalar | Replaces the base value. |
| A different shape than the base (a scalar for a list) | Refused with the path, for example `.linters.enable`. |

An overlay cannot remove a base linter. To silence one in some files, add an exclusion rule: the budget counts it, and a `disable` key would not be counted.

The `nanostack` section belongs to `lintconfig`, which reads it and leaves it out of `.golangci.yml`.

## Adding the standard to existing code

Fix each finding that predates the standard, or exclude it with one rule per linter and file, sorted, with what it hides:

```yaml
linters:
  exclusions:
    rules:
      - {linters: [funlen], path: ^pkg/report/export\.go$} # Export, 70 statements
      - {linters: [govet], path: ^pkg/report/export\.go$} # shadow: err x2
nanostack:
  exclusion-budget: 2
```

`lintconfig` and `lintconfig -check` fail unless `exclusion-budget` equals the number of linters the overlay's rules name. A rule must name its linters and match one path: a path with `|` is refused. A fix deletes a line and must lower the number. A new exclusion must raise it, so a reviewer sees it in the diff.

golangci-lint shows one finding per line by default (`uniq-by-line`). Fixing a finding can uncover a second one on the same line, so lint again after each fix.

## Run it in CI

```yaml
- run: go tool lintconfig -check
- uses: golangci/golangci-lint-action@v9
  with:
    version-file: .golangci-lint-version
    install-only: true
- run: |
    code=0
    golangci-lint run ./... 2>lint.log || code=$?
    cat lint.log >&2
    if grep -q 'Skipped 0 issues by rules' lint.log; then code=1; fi # a stale exclusion
    exit "$code"
```

`warn-unused` prints `Skipped 0 issues by rules` for an exclusion that hides nothing, but exits 0, so the last step fails on it. Run it on all of `./...`: on fewer packages, every rule for the others warns too. If the warning names the base rule for `_test\.go`, the service's tests have no finding it hides; report it to the framework. The framework's own [workflow](../.github/workflows/ci.yml) runs these steps.

## Generic methods and the smoke test

Go 1.27.0 to 1.27.3 write export data that loses the order of generic methods ([golang/go#81188](https://github.com/golang/go/issues/81188)). Linters that read facts about an imported package can then match a fact to the wrong method, and staticcheck crashes in SA4023 and nilness. The bug is on the importer side, so excluding the package that declares generic methods does not contain it. Until the compiler fix ([CL 855582](https://go.dev/cl/855582)) ships in Go 1.27.4, the base disables staticcheck `SA4023` and govet `nilness`, each marked `TEMP(golang/go#81188)`. golangci-lint v2.15.0 or later is required: it bundles staticcheck 2026.2.2 (v0.8.2), the first release that reads Go 1.27 export data.

[`smoke/`](smoke) holds two packages: `listx` declares generic methods before ordinary ones, on a generic receiver and, in `Probe`, on a non-generic one with the method shape of the [go-tools#1746](https://github.com/dominikh/go-tools/issues/1746) reproducer; `consumer` calls both kinds from another package. `smoke_test.go`, behind the `lintsmoke` build tag, checks that the golangci-lint on `PATH` (or named by `GOLANGCI_LINT`) is the pinned release, then runs it on:

1. `consumer` alone, so `listx` arrives from export data;
2. the whole fixture;
3. `consumer` alone again, so the linters read `listx`'s facts from the cache. go-tools#1746 fires only on that warm path: a cold run type-checks `listx` from source and keeps facts keyed by the real objects;
4. `pkg/functional`, the framework's real generic methods, and `pkg/db/transactor`, which imports them;
5. `consumer` while the test holds golangci-lint's global lock, the way a developer's or an editor's lint holds it.

Each run fails on a non-zero exit, on a crash marker such as `panic:` in the output, and on a run longer than 3 minutes, which it stops with `SIGQUIT` so the failure shows every goroutine. The runs pass `--allow-parallel-runners`: without it a run exits when another golangci-lint holds the lock, and with `--allow-serial-runners` it waits, so an editor's lint that holds the lock for minutes would be reported as a hang. Output that still names the lock fails with a message that says so, not as a lint failure.

```sh
go test -tags lintsmoke -count=1 ./lint/smoke/
```

The smoke test guards against linter crashes and hangs on generic methods. It does not prove golang/go#81188 fixed: with golangci-lint v2.15.0 on Go 1.27.3, neither the fixture nor the go-tools#1746 shape crashes even with `SA4023` and `nilness` enabled, cold or warm, and the runs use the repository configuration, where both checks are off. A pass on Go 1.27.4 therefore says nothing about the fix.

A service that adopts this base must delete any exclusion in its `.golangci.local.yml` that only hid a nilness or SA4023 finding, and lower its budget; otherwise its lint step fails on the unused rule.

To enable the checks again:

1. Pin `toolchain go1.27.4` (or later) in `go.mod`.
2. Delete both `TEMP(golang/go#81188)` entries in `base.golangci.yml` and run `go run ./lint/cmd/lintconfig`.
3. Lint the framework and each service with `golangci-lint run ./...`. That full lint, with both checks on, is the test of the fix. Each service fixes or excludes the nilness and SA4023 findings it gets; the framework has none (the impossible `nil` check in `pkg/ptr/ptr_test.go` that the removed exclusion hid is gone).
4. Run the smoke test, and record the change in the [decision log](../docs/decision-log.md).

## Changing the standard

Edit `base.golangci.yml`, run `go run ./lint/cmd/lintconfig` and the framework lint, then release the framework. Moving `golangci-lint-version` to a new minor release can add findings, so lint each service while adopting it.
