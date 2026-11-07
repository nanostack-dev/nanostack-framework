# Publish and adopt a framework version

The framework ships as a Go module; production deployment happens in consuming applications. The repository currently has no automated release workflow.

1. Review public API compatibility, the [existing decisions](../adr/README.md) and affected consumer imports. Run the baseline and relevant integration checks in [testing](../development/testing.md).
   Check compatibility with a consumer build: in each consumer module, run `go mod edit -replace github.com/nanostack-dev/nanostack-framework=<path to the framework change>`, then `go build ./...` and `go vet ./...`. A compile error is a breaking change; record it under breaking changes below. Revert the consumer's `go.mod` and `go.sum` afterwards.
   Use the consumer build instead of `golang.org/x/exp/cmd/apidiff`. apidiff recurses without end on generic methods such as `Option[T].Map[R any]` in `pkg/functional`: one run used 139 GB of memory before it was killed. See the [decision log](../decision-log.md).
2. Merge an approved change and select an unused semantic version. Verify the fetched `origin/main` commit and the existing tags before creating a new immutable `vX.Y.Z` tag and pushing that tag.
3. In a consumer module, run `go get github.com/nanostack-dev/nanostack-framework@vX.Y.Z` and `go mod tidy`; review both module files. Remove temporary local replaces when verifying the published dependency.
4. Run the consumer's affected lint, integration/contract suites and build. Coordinate API migrations across framework packages and Fx modules so type identities agree.
5. Deploy the application through its runbook and verify its health and the adopted behavior. A framework tag alone is not deployment evidence.

Existing versions remain immutable. Document breaking changes and migration steps in the owning capability guide and companion consumer PRs. Private-module access and publication permissions remain repository/account concerns, not shared-workspace prerequisites.
