package nano

// The declarations in this file describe a service's object graph for
// nano gen wire. They are plain data: calling them does nothing at runtime,
// and the generator reads their arguments from source.
//
// A manifest is a package-level variable in a file guarded by the nanogen
// build tag, so it never links into the service:
//
//	//go:build nanogen
//
//	package billing
//
//	var App = nano.Manifest("billing",
//		nanopg.Module,
//		nano.Providers(
//			invoice.NewService,
//			invoice.NewPGStore,
//		),
//		nano.Bind[invoice.Store, *invoice.PGStore](),
//	)
//
// A module is a package-level variable in ordinary code:
//
//	var Module = nano.Module("pg", nano.Providers(NewPool, NewSQLDB))
//
// Providers are package-level functions shaped func(deps...) T,
// func(deps...) (T, error) or func(deps...) (T, func(), error). The generator
// also provides context.Context (Build's ctx), *nano.App, the manifest
// package's Config type and each of its exported fields. See
// docs/generation.md for the whole manifest guide.

// Item is one entry of a manifest or module.
type Item struct {
	kind string
	name string
}

// Spec is an app manifest. It is the value of a manifest variable.
type Spec struct {
	name  string
	items []Item
}

// Manifest declares an app named name with its modules and providers.
func Manifest(name string, items ...Item) Spec {
	return Spec{name: name, items: items}
}

// Module groups items under a name so a library can ship them as one value.
// Providers of an included module are constructed only when something the
// app runs needs them.
func Module(name string, _ ...Item) Item {
	return Item{kind: "module", name: name}
}

// Providers lists constructor functions. In the manifest's own package, an
// unused provider is a generation error; in a library module it is skipped.
func Providers(_ ...any) Item {
	return Item{kind: "providers"}
}

// Bind satisfies the interface I with the provided T. Interface bindings are
// explicit: providing a *PGStore does not by itself satisfy Store.
func Bind[I, T any]() Item {
	return Item{kind: "bind"}
}

// Collect provides []T: one element per listed function, in declaration
// order across the manifest and its modules. Each function is a provider
// whose result is assignable to T. It is a member of the collection only:
// it does not provide its own result type, so several members may return the
// same type. Collect replaces fx value groups:
//
//	// in the middleware module
//	nano.Collect[logging.Enricher](requestlog.NewLogEnricher)
//	// in the auth module
//	nano.Collect[logging.Enricher](auth.NewLogEnricher)
//	// a consumer takes the whole slice
//	func NewLogger(enrichers []logging.Enricher) *slog.Logger
//
// Membership is explicit, like Bind: a provider that returns a T does not
// join the collection by itself. To add a value something else provides, list
// a function that takes it and returns it. Collect[T]() with no function
// declares an empty collection, so a library can take []T whether or not
// the app contributes.
func Collect[T any](_ ...any) Item {
	return Item{kind: "collect"}
}

// Invoke calls fn after every component is constructed, in declaration
// order. fn takes provided values and returns nothing or an error. Invoke is
// for one-off setup that belongs to no component, such as registering routes.
func Invoke(_ any) Item {
	return Item{kind: "invoke"}
}
