// The fixture of the db step tests of the pipeline: its own module, so the
// declaration type-checks wherever a test copies it.
module example.com/dbapp

go 1.27

require (
	github.com/nanostack-dev/nanostack-framework v0.0.0
	github.com/nanostack-dev/nanostack-framework/db v0.0.0
	github.com/nanostack-dev/nanostack-framework/pkg/functional v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
)

replace (
	github.com/nanostack-dev/nanostack-framework => ../../../..
	github.com/nanostack-dev/nanostack-framework/db => ../../../../db
	github.com/nanostack-dev/nanostack-framework/pkg/functional => ../../../../pkg/functional
)
