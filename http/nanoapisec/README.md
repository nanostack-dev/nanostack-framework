# http/nanoapisec

The nano module that builds the security-requirement resolver of `pkg/apisec`. It replaces the fx module `modules/apisec`.

- **Provides:** `*apisec.Resolver`. Pick the module that matches the router:
  - `ChiModule` (`NewChiResolver(Document)`) matches as chi. It is the drop-in replacement of `modules/apisec`, which matched as chi. echopoint and anchor route with chi.
  - `Module` (`NewResolver(Document)`) matches as `http.ServeMux` and `nanohttp.NewRouter`.
  - A router with `RouterOptions.ExplicitHEAD` provides its resolver from `NewResolverWith(Document, apisec.ExplicitHEAD(true))`.

  The two routers disagree on an escaped path (`/files/%61dmin`) and on sibling precedence. A resolver that disagrees with the router checks the wrong operation's security.
- **Needs:** a `Document`, the raw OpenAPI contract. The service provides it from a provider that returns its embedded spec. This replaces `fx.Supply(apisec.Document(spec))`.
- **Lifecycle:** none. The resolver is built when the App is built, so a contract that the resolver cannot load or index fails the start.
- **Config:** none.

```go
//go:embed openapi.yaml
var openapiSpec []byte

func OpenAPIDocument() nanoapisec.Document { return openapiSpec }

var App = nano.Manifest("api",
	nanoapisec.ChiModule, // the router is chi; nanoapisec.Module for nanohttp.NewRouter
	nano.Providers(OpenAPIDocument),
	// ...
)
```

`pkg/apisec` is on the integration allow-list (`internal/layers.AllowedLegacy`) until it moves to `http/`. See [docs/modules.md](../../docs/modules.md) for the migration from fx.
