// The fixture of the pipeline tests: its own module, so the paths nano gen
// writes into headers do not depend on where a test copies it.
module example.com/pipelineapp

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
