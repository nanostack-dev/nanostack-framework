// A module that does not require the root framework module: the base that
// nanostack gen repo writes imports pkg/db/transactor, which it cannot load.
module example.com/module

go 1.27

require github.com/nanostack-dev/nanostack-framework/db v0.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/nanostack-dev/nanostack-framework/pkg/functional v0.0.0 // indirect
)

replace (
	github.com/nanostack-dev/nanostack-framework/db => ../../../../../db
	github.com/nanostack-dev/nanostack-framework/pkg/functional => ../../../../../pkg/functional
)
