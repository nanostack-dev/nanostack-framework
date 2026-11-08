// Package nanoapisec is the nano module that builds the security-requirement
// resolver of pkg/apisec from the service's OpenAPI document.
//
// It replaces the deprecated fx module modules/apisec. A service lists Module
// in its manifest and provides its embedded contract as a Document, where it
// supplied apisec.Document with fx.Supply before:
//
//	//go:embed openapi.yaml
//	var openapiSpec []byte
//
//	func OpenAPIDocument() nanoapisec.Document { return openapiSpec }
//
// The resolver is built when the App is built, so a contract that the resolver
// cannot index fails the start instead of leaving routes unguarded.
package nanoapisec
