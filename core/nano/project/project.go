// Package project declares the inputs of a module's code generation that
// nano gen cannot discover. A module holds one declaration, a package-level
// variable named Project in nanogen/project.go:
//
//	// Package nanogen declares the inputs of this module's code generation
//	// that cannot be discovered. Run: go tool nano gen
//	package nanogen
//
//	import "github.com/nanostack-dev/nanostack-framework/core/nano/project"
//
//	var Project = project.Project{
//		API: &project.OpenAPI{
//			Spec:   "cmd/http/openapi.yaml",
//			Server: project.Package{Dir: "internal/api"},
//			Clients: []project.Client{
//				{Dir: "cmd/it/client", Package: "client", PlainTypes: true},
//			},
//		},
//		Postgres: &project.Postgres{Name: "anchor"},
//		Env:      "../../.env.example",
//	}
//
// The declaration is plain data, like nano.Manifest: nothing calls it and no
// binary imports the nanogen package. The file carries no build tag, so
// go build, go vet and gopls type-check it, and a misspelled field is a
// compile error. nano gen reads the composite literal from source: every
// value must be a literal or a constant.
//
// Everything else is discovered: //nano:errors catalogues, and the packages
// whose //go:build nanogen file calls nano.Manifest, for config and wire.
// A module without the file still runs the discovered steps.
//
// The types live in the root module, not in the gen module, because the
// root module never requires gen and its own example service declares a
// project too (ADR 0023).
package project

// Project is the declaration of one module's generation inputs. Every path is
// relative to the directory nano gen runs on, the module root by default.
type Project struct {
	// API is the OpenAPI document and the packages generated from it. Nil
	// when the module has no OpenAPI document; the api and validate steps
	// then have nothing to run on.
	API *OpenAPI
	// Postgres is the database whose go-jet models and table types nano gen
	// db generates from the migrations. Nil when the module has no database
	// code to generate; the db step then has nothing to run on.
	Postgres *Postgres
	// Env is the .env.example file that nano gen config writes. Empty
	// writes .env.example next to the Config type.
	Env string
}

// Postgres is a PostgreSQL database built from SQL migrations. nano gen db
// applies the migrations to an empty database, on a container it starts or
// on the server NANO_GEN_DATABASE_URL names, and writes the go-jet code of
// Schema to <Out>/<Name>/<Schema>/{model,table,view,enum}, with
// <Out>/<Name>/nanogen.sum. Each table type also gets the methods
// UniqueKeys (the keys that match at most one row), ColumnTypes (the
// catalogue type of each column) and ForeignKeys. While the migrations, the
// image and every generated file match nanogen.sum, a run needs no database.
type Postgres struct {
	// Name is the directory under Out, by convention the database name:
	// "anchor" writes internal/db/gen/anchor. Required.
	Name string
	// Migrations is the directory of golang-migrate SQL migrations
	// (<version>_<title>.up.sql). Default "migrations".
	Migrations string
	// Out is the directory that holds Name. Default "internal/db/gen".
	Out string
	// Schema is the schema to generate. Default "public".
	Schema string
	// Image is the container image that nano gen db starts, such as
	// "timescale/timescaledb:2.23.0-pg18" when the migrations need an
	// extension. Default "postgres:18-alpine".
	Image string
	// Scopes are the tenant columns, such as "platform_tenant_id" or
	// "product_id". A table is scoped when it has one of them as a NOT NULL
	// column. nano gen repo warns on a hand-written query of a scoped table,
	// built on its generated base, that does not filter on the column,
	// unless the method name ends in Internal. Empty: no table is scoped.
	// nano gen repo also proves that every generated search covers each of
	// them (ADR 0027): a column that is not in ContextScopes is a hard scope,
	// whose value the caller passes in the scope value of the search.
	Scopes []string
	// ContextScopes are the tenant columns of Scopes whose value comes from
	// the request context, such as a workspace that a header selects. A
	// search scopes such a column only with nanorepo.ScopeFunc and the
	// function registered here; the function can return TRUE for a read
	// across the column, and must not return nil. Empty: every tenant column
	// is a hard scope.
	ContextScopes []ContextScope
}

// ContextScope registers the function that scopes one context tenant
// column, for example:
//
//	{Column: "workspace_id", Func: "example.com/app/internal/db/workspacescope.Filter"}
type ContextScope struct {
	// Column is a column of Postgres.Scopes.
	Column string
	// Func is the import path of the function's package, a dot and the
	// function's name. The function has the form
	// func(context.Context, C) B: C is a column type and B a boolean
	// expression.
	Func string
}

// OpenAPI is the API contract and the Go packages generated from it. nano
// gen api runs oapi-codegen v2.8.2 in-process on Spec, with the framework's
// server templates, and writes the server and each client.
type OpenAPI struct {
	// Spec is the OpenAPI 3.0 or 3.1 document. It may sit outside the
	// directory nano gen runs on, such as the spec of a service in a client
	// module: "../../apps/anchor/cmd/http/openapi.yaml".
	Spec string
	// Server is the package that holds the models and the strict server on
	// net/http's ServeMux. nano gen api also writes handlers.gen.go there,
	// the split of the server by OpenAPI tag, and nano gen validate writes
	// validate.gen.go. A spec with a server tags every operation (ADR 0024).
	// Its package name is the one its Go files declare, or the directory
	// name when it has none. Zero in a module that holds only clients.
	Server Package
	// Clients are the Go clients generated from Spec, after the server.
	Clients []Client
	// SortByDirection is the direction of a search request that sends a
	// sort_by and no sort_direction, as the API contract states it: "ASC"
	// or "DESC". APIs differ here, so there is no default. nano gen repo
	// writes it into each generated API mapper (<Name>SearchFromAPI). It is
	// required when the request body of a nanorepo.Search that declares API
	// has a sort_by property.
	SortByDirection string
}

// Package is a Go package directory and the file generated into it.
type Package struct {
	// Dir is the package directory.
	Dir string
	// File is the generated file in Dir. Default api.gen.go. It may not be
	// handlers.gen.go or validate.gen.go, which other outputs use.
	File string
}

// Client is a Go client of the API: models and a client type, in one file.
type Client struct {
	// Dir is the package directory. nano gen creates it when it is missing.
	// A client holds the models of the whole spec, so it needs a directory
	// of its own, apart from the server and the other clients.
	Dir string
	// Package is the package name. A directory such as clients/go has no
	// usable name, so the declaration states it.
	Package string
	// File is the generated file in Dir. Default client.gen.go.
	File string
	// PlainTypes drops the server's Go type hints from the client: every
	// x-go-type except "string", every x-go-type-import, and the
	// x-go-type-skip-optional-pointer next to an x-go-type of the
	// nullable package. The client then uses plain types and the
	// pointers that can send null.
	PlainTypes bool
}
