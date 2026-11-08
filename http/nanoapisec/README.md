# http/nanoapisec

The nano module that builds the security-requirement resolver of `pkg/apisec`. It replaces the fx module `modules/apisec`.

- **Provides:** `*apisec.Resolver` from `NewResolver(Document)`.
- **Needs:** a `Document`, the raw OpenAPI contract. The service provides it from a provider that returns its embedded spec. This replaces `fx.Supply(apisec.Document(spec))`.
- **Lifecycle:** none. The resolver is built when the App is built, so a contract that the resolver cannot load or index fails the start.
- **Config:** none.

```go
//go:embed openapi.yaml
var openapiSpec []byte

func OpenAPIDocument() nanoapisec.Document { return openapiSpec }

var App = nano.Manifest("api",
	nanoapisec.Module,
	nano.Providers(OpenAPIDocument),
	// ...
)
```

`pkg/apisec` is on the integration allow-list (`internal/layers.AllowedLegacy`) until it moves to `http/`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
