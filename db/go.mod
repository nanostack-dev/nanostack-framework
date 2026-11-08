module github.com/nanostack-dev/nanostack-framework/db

go 1.27

toolchain go1.27.3

// used by jet generator
require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.12.3
	github.com/mattn/go-sqlite3 v1.14.50
	github.com/nanostack-dev/nanostack-framework/pkg/functional v0.0.0 // Nanostack: functional.Option support
)

// used in tests
require (
	github.com/google/go-cmp v0.7.0
	github.com/stretchr/testify v1.12.1
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

// Development form (RELEASING.md, "go.mod rules"): main requires the module of this repository
// at v0.0.0 and replaces it with its directory. The release commit requires the tag instead.
replace github.com/nanostack-dev/nanostack-framework/pkg/functional => ../pkg/functional
