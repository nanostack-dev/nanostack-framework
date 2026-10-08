# Local setup

An independent clone is sufficient. Use the Go version and toolchain from [go.mod](../../go.mod), currently Go 1.27, because the functional APIs use generic methods.

```sh
go mod download
go test ./...
go build ./...
```

Private repository/module access depends on your Git credentials; configure `GOPRIVATE` for the private Nanostack modules used by the consuming application when needed. Keep credential values out of committed files.

The default tests exercise pure code and in-process fixtures. Database integration tests need a disposable PostgreSQL instance and the explicit DSNs described in [testing](testing.md). Consumer applications have their own startup, generation and service dependencies.

There is no runnable framework `nanostack new` or `nanostack doctor` CLI in the current tree. [cli/README.md](../../cli/README.md) describes direction, not an installation prerequisite. Generation helpers also do not replace a consumer's own OpenAPI or database generation commands.
