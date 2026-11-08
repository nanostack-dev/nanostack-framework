module github.com/nanostack-dev/nanostack-framework/gen

go 1.27

toolchain go1.27.3

require (
	github.com/getkin/kin-openapi v0.149.0
	github.com/nanostack-dev/nanostack-framework v0.0.0
	github.com/oapi-codegen/nullable v1.1.0
	github.com/oapi-codegen/oapi-codegen/v2 v2.8.2
	github.com/oapi-codegen/runtime v1.8.0
	golang.org/x/tools v0.52.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dprotaso/go-yit v0.0.0-20220510233725-9ba8df137936 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/speakeasy-api/jsonpath v0.6.3 // indirect
	github.com/speakeasy-api/openapi v1.24.0 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// The root module is a module of this repository. Until a release tags it, gen uses
// the copy in this repository. The root requires db and pkg/functional at v0.0.0,
// and a dependency's replace does not apply here, so its replaces are repeated.
// The release removes these lines (RELEASING.md).
replace (
	github.com/nanostack-dev/nanostack-framework => ../
	github.com/nanostack-dev/nanostack-framework/db => ../db
	github.com/nanostack-dev/nanostack-framework/pkg/functional => ../pkg/functional
)
