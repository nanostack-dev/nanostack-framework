package apisec_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	modapisec "github.com/nanostack-dev/nanostack-framework/modules/apisec"
	"github.com/nanostack-dev/nanostack-framework/pkg/apisec"
)

// TestModuleKeepsItsFxSignature builds the resolver the v0.22.0 way: the
// service supplies apisec.Document and takes *apisec.Resolver.
func TestModuleKeepsItsFxSignature(t *testing.T) {
	var resolver *apisec.Resolver
	app := fxtest.New(t,
		fx.Supply(modapisec.Document(`openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /health:
    get:
      responses: {"200": {description: ok}}
`)),
		modapisec.NewModule(),
		fx.Populate(&resolver),
	)
	app.RequireStart().RequireStop()
	if reqs, ok := resolver.For(httptest.NewRequest(http.MethodGet, "/health", nil)); !ok || !reqs.Public() {
		t.Errorf("For(GET /health) = %v, %v; want a public operation", reqs, ok)
	}
}

func TestModuleFailsTheAppOnAMalformedDocument(t *testing.T) {
	app := fx.New(fx.NopLogger,
		fx.Supply(modapisec.Document("openapi: [")),
		modapisec.NewModule(),
		fx.Invoke(func(*apisec.Resolver) {}),
	)
	if app.Err() == nil {
		t.Fatal("the app started with a malformed document")
	}
}

// An fx service routes with chi, so the module matches as chi does: a
// document whose paths a ServeMux cannot hold together builds, and an
// escaped literal segment reaches the templated operation, as chi routes it.
func TestModuleMatchesAsChi(t *testing.T) {
	var resolver *apisec.Resolver
	app := fxtest.New(t,
		fx.Supply(modapisec.Document(`openapi: 3.0.3
info: {title: t, version: "1"}
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
paths:
  /flows/{id}/variables:
    delete:
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      security: [{bearer: []}]
      responses: {"204": {description: ok}}
  /flows/folders/{id}:
    delete:
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      security: []
      responses: {"204": {description: ok}}
  /files/{name}:
    get:
      parameters: [{name: name, in: path, required: true, schema: {type: string}}]
      security: [{bearer: []}]
      responses: {"200": {description: ok}}
  /files/admin:
    get:
      security: []
      responses: {"200": {description: ok}}
`)),
		modapisec.NewModule(),
		fx.Populate(&resolver),
	)
	app.RequireStart().RequireStop()
	if reqs, ok := resolver.For(httptest.NewRequest(http.MethodGet, "/files/%61dmin", nil)); !ok || reqs.Public() {
		t.Errorf("For(GET /files/%%61dmin) = %v, %v; want the protected template, as chi routes it", reqs, ok)
	}
	if reqs, ok := resolver.For(
		httptest.NewRequest(http.MethodDelete, "/flows/folders/variables", nil),
	); !ok ||
		!reqs.Public() {
		t.Errorf("For(DELETE /flows/folders/variables) = %v, %v; want chi's pick, the public folder route", reqs, ok)
	}
}
