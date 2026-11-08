// The fixture of the db step tests of the pipeline: its own module, so the
// declaration type-checks wherever a test copies it.
module example.com/dbapp

go 1.27

require github.com/nanostack-dev/nanostack-framework v0.0.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/nanostack-dev/nanostack-framework => ../../../..
	github.com/nanostack-dev/nanostack-framework/db => ../../../../db
	github.com/nanostack-dev/nanostack-framework/pkg/functional => ../../../../pkg/functional
)
