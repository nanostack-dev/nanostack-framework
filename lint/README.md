# Lint standard

One golangci-lint configuration for every Nanostack Go service. The framework owns the base. A service owns only its additions, and commits the merged result so CI, editors and the golangci-lint GitHub Action read an ordinary `.golangci.yml`.

| File | Owner | Content |
|---|---|---|
| [`base.golangci.yml`](base.golangci.yml) | framework | 33 linters and their settings |
| [`golangci-lint-version`](golangci-lint-version) | framework | the golangci-lint release the base is written for |
| `.golangci.local.yml` | service | module path, ruleguard files, service exclusions |
| `.golangci.yml`, `.golangci-lint-version` | generated | committed; never edited by hand |

golangci-lint has no `extends`, and its maintainers declined remote configuration ([#2137](https://github.com/golangci/golangci-lint/issues/2137)). The choice of linters and the alternatives are in the [research](../docs/research/golangci-lint-standard.md).

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

An overlay cannot remove a base linter. To silence one in some files, add an exclusion rule.

## Adding the standard to existing code

Each finding that predates the standard is fixed or listed in the overlay's exclusion rules, by linter and path. `warn-unused` reports a rule with nothing left to exclude, so the list only shrinks; nothing new joins it. The framework's own [`.golangci.local.yml`](../.golangci.local.yml) shows the shape.

## Changing the standard

Edit `base.golangci.yml`, run `go run ./lint/cmd/lintconfig` and the framework lint, then release the framework. Moving `golangci-lint-version` to a new minor release can add findings, so lint each service while adopting it.
