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
