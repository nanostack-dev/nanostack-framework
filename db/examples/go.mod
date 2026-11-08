module github.com/nanostack-dev/nanostack-framework/db/examples

go 1.27

require (
	github.com/lib/pq v1.12.3
	github.com/nanostack-dev/nanostack-framework/db v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/nanostack-dev/nanostack-framework/pkg/functional v0.0.0 // indirect
)

replace github.com/nanostack-dev/nanostack-framework/db => ../

// Nanostack: db requires pkg/functional, which is not tagged yet (see UPSTREAM.md
// "Release order"). A dependency's replace does not apply here, so repeat it.
replace github.com/nanostack-dev/nanostack-framework/pkg/functional => ../../pkg/functional
