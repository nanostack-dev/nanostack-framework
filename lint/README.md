# Lint standard

One golangci-lint configuration for every Nanostack Go service. The framework owns the base. A service owns only its additions, and commits the merged result so CI, editors and the golangci-lint GitHub Action read an ordinary `.golangci.yml`.

| File | Owner | Content |
|---|---|---|
| [`base.golangci.yml`](base.golangci.yml) | framework | 33 linters and their settings |
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

## Changing the standard

Edit `base.golangci.yml`, run `go run ./lint/cmd/lintconfig` and the framework lint, then release the framework. Moving `golangci-lint-version` to a new minor release can add findings, so lint each service while adopting it.
