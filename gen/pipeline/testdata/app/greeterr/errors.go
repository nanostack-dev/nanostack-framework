// Package greeterr is the error catalogue of the pipeline fixture.
package greeterr

// declarations is the greeter error catalogue.
//
//nano:errors namespace=greeter
type declarations interface {
	// The greeting text is empty.
	//
	//nano:error code=greeting_empty status=400 title="Greeting is empty"
	GreetingEmpty()
}
