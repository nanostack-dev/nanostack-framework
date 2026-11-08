# Architecture

> **History.** This page is the original direction, kept for context. It is not updated as code lands. The current packages, the three layers (`core/`, `http/`, `gen/` with `cmd/nano`) and their import rules are in [Implemented architecture](technical/architecture.md) and [Boundaries](boundaries.md).

## Framework Areas

```text
Nanostack Framework
├── app
│   └── fluent service composition API
├── modules
│   ├── config
│   ├── logging
│   ├── postgres
│   ├── cache
│   ├── migrations
│   ├── pglock
│   ├── httpserver
│   ├── pprof
│   ├── pgqueue
│   └── workflow
├── pkg
│   ├── ids
│   ├── crypto
│   ├── secrets
│   ├── apierror
│   ├── health
│   ├── search
│   ├── jetx
│   ├── httputil
│   └── testkit
├── gen
│   ├── OpenAPI generation conventions
│   ├── client generation conventions
│   ├── go-jet generation conventions
│   └── schema/type mapping helpers
├── starter
│   ├── service template
│   ├── baseline folders
│   └── example modules
└── cli
    ├── scaffold commands
    ├── generate commands
    ├── doctor/validation commands
    └── upgrade assistance
```

## Relationship To `nanostack-shared`

`nanostack-shared` should become a temporary compatibility bridge while its contents move into `modules/` and `pkg/`.

The current shared repository should not remain a generic `toolkit` bucket. Its contents should be reorganized into bounded packages such as:

- `ids`
- `secrets`
- `validate`
- `health`
- `apierror`
- `jetx`
- `search`
- `modules/*`

Compatibility packages may exist temporarily during migration.

## Dependency Rules

1. `pkg/` leaf packages should not depend on FX.
2. `modules/` packages should be thin wiring wrappers around reusable primitives and third-party lifecycle integrations.
3. Search DTO packages should not depend on Jet.
4. HTTP helper packages should not directly own application-specific middleware logic.
5. Generation concerns should be separated from service runtime concerns.

## Proposed Repository Areas

Initial repository structure:

```text
app/
modules/
pkg/
gen/
cli/
starter/
docs/
```

The first milestone does not require all areas to be implemented. It does require the boundaries to be explicit.
