# Research: one golangci-lint standard for Nanostack services

- Date: 2026-10-29
- Outcome: implemented in [`lint/`](../../lint/README.md)
- Local evidence: [appendix](#appendix-local-evidence)

## In 30 seconds

- **Go from 72 linters to 33.** echopoint enables 71 and anchor 70. Across 19 large Go projects that list their linters, the median is 16 ([counts](#what-large-projects-enable)). We keep more than the median because we are a database and HTTP backend with architecture rules enforced by lint ([ADR 0012](https://github.com/nanostack-dev/nanostack-framework/pull/59)).
- **Remove 41, add 2.** 5 of the removed linters have nothing to check in our code, 7 duplicate another linter, and 29 are style rules or rare bug classes that almost no large project enables.
- **Ship it from the framework.** golangci-lint v2 has no `extends`, and its maintainers declined remote configs ([#2137](https://github.com/golangci/golangci-lint/issues/2137), [#3954](https://github.com/golangci/golangci-lint/discussions/3954)). The framework owns a base file; each service adds a small overlay, commits the merged result, and CI fails when it is stale.

## Keep (33)

| Group | Linters | Why |
|---|---|---|
| Base (5) | errcheck, govet, ineffassign, staticcheck, unused | The `standard` set ([quick start](https://golangci-lint.run/docs/welcome/quick-start/)); in 16–19 of the 19 projects. |
| Bugs in our domain (15) | bodyclose, noctx, rowserrcheck, sqlclosecheck, errorlint, nilerr, nilnesserr, durationcheck, fatcontext, spancheck, exhaustive, gosec, bidichk, testifylint, usetesting | HTTP bodies and context, `database/sql` rows (48 echopoint files, 38 anchor files), wrapped errors, OpenTelemetry spans, enum switches, security, testify (294 and 208 files). |
| Our conventions (13) | revive, depguard, gocritic, exhaustruct_v5, funlen, gocognit, nolintlint, forbidigo, modernize, unconvert, usestdlibvars, unparam, misspell | ADR 0012 rules (imports, file and function size, complete `Deps`), ruleguard rules in `gorules/`, honest `nolint`, Go modernization with autofix. |

New: `misspell` (12 of 19 projects) and `exhaustruct_v5` limited to `Deps` types ([ADR 0010](https://github.com/nanostack-dev/nanostack-framework/pull/59)). `exhaustruct` is deprecated in favour of `exhaustruct_v5` since v2.13 ([linters](https://golangci-lint.run/docs/linters/)). anchor gains `funlen` and `gocognit`.

## Remove (41)

| Reason | Linters |
|---|---|
| Nothing to check (5) | protogetter (no protobuf), promlinter (no Prometheus metrics), sloglint (no `log/slog`), loggercheck (zap, logr, klog or slog only; we use zerolog), gochecksumtype (no `//sumtype:decl`) |
| Covered by a kept linter (7) | copyloopvar and intrange (`modernize` has `forvar` and `rangeint`, [docs](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/modernize)); cyclop, gocyclo, nestif (one complexity metric is enough: `gocognit`); wastedassign (`ineffassign`, `staticcheck`); asciicheck (`bidichk` covers the dangerous characters) |
| Style, used by 0 of 19 projects (7) | funcorder, gochecknoglobals (27 `nolint` in anchor), testpackage (9 `nolint`), nonamedreturns, recvcheck, musttag, dupl (anchor only, 6 `nolint`) |
| Style or rare bugs, used by 1–3 of 19 (22) | goconst (9 per-file exclusions in echopoint), mnd (8 `nolint`), godot, gochecknoinits, gomoddirectives, errname, iface, canonicalheader, predeclared, mirror, goprintffuncname, testableexamples, tparallel, nosprintfhostport, reassign, makezero, asasalint, gocheckcompilerdirectives, exptostd, nakedret, perfsprint, whitespace |

Each removed linter passes today, so removing it changes no code. One that later catches a real bug can come back with that bug as its reason.

## Settings that change

| Setting | Today | Standard | Why |
|---|---|---|---|
| Exclusion presets | `std-error-handling`, `common-false-positives` and 3 hand-written revive comment rules | add the `comments` preset, delete the 3 rules | The preset covers the same doc-comment rules ([false positives](https://golangci-lint.run/docs/linters/false-positives/)). |
| `exclusions.warn-unused` | off | `true` | Stale per-file exclusions (like the 9 `goconst` ones) get reported. |
| Test files | echopoint turns off 11 linters in `_test.go` and `cmd/it`, including `govet` and `errcheck` | turn off only funlen, gocognit, gosec, noctx, bodyclose | `govet` and `errcheck` find real bugs in tests too. |
| `revive` | one rule disabled, default rules dropped | `enable-default-rules: true`, `file-length-limit: 400`, `var-naming` off | Setting rules drops revive's 23 defaults unless re-enabled ([config](https://golangci-lint.run/docs/linters/configuration/)); 400 lines is the ADR 0012 file budget. |
| `exhaustive` | `check: [switch, map]` | `check: [switch, mapliteral]`, `default-signifies-exhaustive: true` | As in Grafana and Moby. v2.15 renamed `map` to `mapliteral`; the config schema refuses `map` ([changelog](https://golangci-lint.run/docs/product/changelog/)). |
| `gosec` | anchor excludes G117 by text | exclude G101 and G117 everywhere | Secret-name patterns; 2 of our 9 `gosec` `nolint` are this case. Grafana excludes G101 ([config](https://raw.githubusercontent.com/grafana/grafana/main/.golangci.yml)). |
| `issues.max-issues-per-linter`, `max-same-issues` | 50 | 0 | Show every finding, as Kubernetes, Prometheus, Grafana and Moby do. |
| `run.relative-path-mode` | `cfg` (default) | `gomod` | Path rules keep working when the file comes from the framework. Kubernetes uses `gomod`. |
| `run.timeout` | none | `10m` | v2 has no default timeout ([migration](https://golangci-lint.run/docs/product/migration-guide/)). |
| golangci-lint version | v2.14 | v2.15.x, pinned by minor | Minor releases may add findings ([roadmap](https://golangci-lint.run/docs/product/roadmap/)). |

Unchanged: `errcheck.check-type-assertions`, `govet` enable-all minus `fieldalignment` (with `shadow` strict, which passes today), `staticcheck` all minus ST1000/ST1016/QF1008, `nolintlint` with `require-explanation` and `require-specific`, `funlen` 100/50, `gocognit` 20, formatters `goimports` and `golines` at 120.

Adjacent defect: echopoint's ruleguard setting loads `gorules/functional.go` and `gorules/faults.go`, but not `gorules/transactor.go`.

## Sharing one config

golangci-lint reads one file. There is no `extends`, and the maintainers' answer to remote configs is `--config <(curl …)` ([#2137](https://github.com/golangci/golangci-lint/issues/2137)). ldez warns that a common config tends to become weak ([#3954](https://github.com/golangci/golangci-lint/discussions/3954)). Our depguard rules contain each service's module path, so a single shared file cannot hold them.

| Option | Verdict |
|---|---|
| Copy the file into each repo and let it drift | What we have now. |
| `--config` pointing into the pinned framework module | No service overlay possible, and IDEs don't see the file. |
| **Base in the framework + overlay per service, merged and committed** | Chosen. One base, a 20–40 line overlay, a normal `.golangci.yml` for IDEs, and a CI check that it matches. |

```text
nanostack-framework/lint/
  base.golangci.yml        # the standard above, embedded in the lint package
  golangci-lint-version    # v2.15.0
  cmd/lintconfig           # merges base + overlay; -check fails when stale

<service>/
  .golangci.local.yml      # module path, gorules list, rollout exclusions
  .golangci.yml            # generated, committed
  .golangci-lint-version   # generated, read by the golangci-lint GitHub Action
```

A service adds the command with `go get -tool` and runs `go tool lintconfig`. Each service keeps its own lint script; only the configuration and the version are shared.

The framework version in each service's `go.mod` decides which base it gets, so a lint change reaches a service only when it bumps the framework.

## Rollout

1. Framework: add `lint/` and lint the framework itself with the base. It had no lint config: formatter and `modernize` fixes were applied, and about 80 older findings are listed by linter and path in its `.golangci.local.yml`.
2. echopoint, then anchor: generate `.golangci.yml`, run the full lint, and put each new finding either in a fix or in a rollout exclusion in the overlay.
3. The ADR 0012 rules (depguard feature imports, `file-length-limit`, `exhaustruct_v5` on `Deps`) start with an exclusion that lists the features not yet migrated. The list only shrinks.

## Your call

- `nakedret` and `nonamedreturns`: dropped as style. Keep one if you want a rule on named results.
- `perfsprint` and `whitespace`: dropped as low value. Both have autofix, so keeping them costs little.
- Test files with `govet` and `errcheck` on: expect findings in `cmd/it` on the first run.

## What large projects enable

Config files fetched on 2026-10-29 from each default branch. All use config version 2.

| Linters | Projects |
|---|---|
| 5–8 | tailscale (5), vault (6), uber-go/zap (7), consul (8) |
| 11–16 | temporal (11), loki (12), minio (13), kubernetes (14), containerd (15), etcd (16), cilium (16) |
| 20–36 | grafana (20), cli/cli (20), prometheus (23), otel-collector (26), gitea (27), caddy (32), golangci-lint (32), moby (36) |
| `default: all` minus a list | river (24 off), traefik (39 off) |

Most used: govet 19, staticcheck 17, ineffassign 17, unused 16, revive 14, depguard 13, misspell 12, unconvert 12, errcheck 10, testifylint 9, gocritic 8.

- golangci-lint's own config: "We intentionally use a limited set of linters", and it is "not a recommendation" ([config](https://raw.githubusercontent.com/golangci/golangci-lint/main/.golangci.yml)).
- Uber's minimum: errcheck, goimports, revive, govet, staticcheck; consistency matters more than the exact set ([guide](https://github.com/uber-go/guide/blob/master/style.md#lint)).
- Our configs started from maratori's golden config, which calls itself "intentionally strict" and is based on "my experience and opinion" ([repo](https://github.com/maratori/golangci-lint-config)).
- v2 removed 13 deprecated linters and merged gosimple and stylecheck into staticcheck ([migration](https://golangci-lint.run/docs/product/migration-guide/), [v2 post](https://ldez.github.io/blog/2025/03/23/golangci-lint-v2/)).
- Gradual adoption: `--new-from-merge-base` or the action's `only-new-issues` ([FAQ](https://golangci-lint.run/docs/welcome/faq/), [action](https://github.com/golangci/golangci-lint-action)).

## Appendix: local evidence

### Our configs

| Repo | File | Lines | Linters | Formatters | Exclusion rules |
|---|---|---|---|---|---|
| echopoint | `.golangci.yml` | 542 | 71 (`default: none`) | goimports, golines | 22 |
| anchor | `.golangci.yml` | 488 | 70 (`default: none`) | goimports, golines | 18 |
| nanostack-framework | none | – | – | – | – |

- The two lists differ only by `funlen` and `gocognit` (echopoint) and `dupl` (anchor).
- The framework has no lint config at all.
- Both configs come from maratori's golden config (commented "[highly recommend to enable]" entries).
- echopoint excludes `goconst` file by file for 9 files, and turns off `govet`, `errcheck`, `bodyclose`, `noctx` and 6 others for all of `cmd/it` and `_test.go`.

### nolint directives by linter

- anchor: gochecknoglobals 27, gosec 8, testpackage 7, unused 6, dupl 6, mnd 5, nilnil 4, revive 2, nestif 2, unparam 1, staticcheck 1, nonamedreturns 1.
- echopoint: mnd 3, testpackage 2, tparallel 1, nilnil 1, gosec 1.

### Linters with nothing to check in our code

| Linter | Targets | Our imports |
|---|---|---|
| protogetter | protobuf getters | no `google.golang.org/protobuf` import, no `.pb.go` |
| promlinter | Prometheus metric names | no `client_golang` import |
| sloglint | `log/slog` | 0 imports (zerolog: 54 echopoint files, 89 anchor files) |
| loggercheck | zap, logr, klog, kitlog, slog key/value pairs | none of them imported; zerolog is not covered |
| gochecksumtype | `//sumtype:decl` interfaces | 0 declarations |

Domain linters that do apply: `rowserrcheck` and `sqlclosecheck` (`database/sql` in 48 echopoint and 38 anchor files), `testifylint` (testify in 294 and 208 files), `spancheck` (OpenTelemetry in echopoint since #680).

### What 21 large Go projects enable (config files fetched 2026-10-29 from each default branch)

Projects: kubernetes (`hack/golangci.yaml`), prometheus, grafana, loki, temporal (`.github/.golangci.yml`), etcd (`tools/.golangci.yaml`), moby, traefik, gitea, consul, vault, golangci-lint, opentelemetry-collector, tailscale, uber-go/zap, cli/cli, caddy, minio, containerd, cilium, river.

- All 21 use config version 2.
- 19 list their linters (`default: none` or `standard` plus `enable`). Count of linters: 5 (tailscale), 6 (vault), 7 (zap), 8 (consul), 11 (temporal), 12 (loki), 13 (minio), 14 (kubernetes), 15 (containerd), 16 (etcd, cilium), 20 (grafana, cli), 23 (prometheus), 26 (otel-collector), 27 (gitea), 32 (caddy, golangci-lint), 36 (moby). Median: 16.
- 2 use `default: all` with a disable list: river (24 disabled), traefik (39 disabled).

Usage of each linter across the 19 explicit configs:

- 19 govet; 17 staticcheck, ineffassign; 16 unused; 14 revive; 13 depguard; 12 misspell, unconvert; 10 errcheck; 9 testifylint; 8 gocritic.
- 7 modernize, whitespace, gosec, copyloopvar, forbidigo, errorlint; 6 nolintlint, usetesting.
- 5 bodyclose, perfsprint, nakedret, unparam, usestdlibvars; 4 bidichk, exhaustive, dogsled, durationcheck, asasalint, importas, fatcontext, gocheckcompilerdirectives.
- Ours used by none of the 19: cyclop, funcorder, gochecknoglobals, gocognit, musttag, nestif, nonamedreturns, protogetter, recvcheck, rowserrcheck, spancheck, testpackage.
- Ours used by 1–2: canonicalheader, errname, funlen, gochecknoinits, gochecksumtype, goconst, gocyclo, godot, gomoddirectives, iface, intrange, loggercheck, makezero, mirror, mnd, nilerr, nilnesserr, noctx, nosprintfhostport, predeclared, promlinter, reassign, sqlclosecheck, testableexamples, tparallel.
- Common but not ours: misspell (12), dogsled (4), importas (4).
