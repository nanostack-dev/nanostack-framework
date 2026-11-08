# Publish and adopt a framework version

The framework ships as three Go modules: the root module, `db` and `pkg/functional`. Production deployment happens in consuming applications.

[RELEASING.md](../../RELEASING.md) is the runbook for versions and tags: the `go.mod` rules, the leaves-first tag order, the exact commands, the verification and the rollback. `.github/workflows/release.yml` checks the `go.mod` files on every pull request and builds a scratch consumer for each pushed tag. This page keeps the compatibility check and the adoption steps around a release.

1. Review public API compatibility, the [existing decisions](../adr/README.md) and affected consumer imports. Run the baseline and relevant integration checks in [testing](../development/testing.md).
   Check compatibility with a consumer build. A consumer applies only its own `replace` lines, and on `main` the root module requires `db` and `pkg/functional` at the placeholder `v0.0.0`. So replace all three released modules in each consumer module:

   ```sh
   F=<path to the framework change>
   go mod edit \
     -replace github.com/nanostack-dev/nanostack-framework=$F \
     -replace github.com/nanostack-dev/nanostack-framework/db=$F/db \
     -replace github.com/nanostack-dev/nanostack-framework/pkg/functional=$F/pkg/functional
   go mod tidy && go build ./... && go vet ./...
   ```

   A replace of the root module alone resolves `db` and `pkg/functional` from tags or from the local module cache, so it can fail with an unknown revision or test the wrong code. A compile error is a breaking change; record it under breaking changes below. Revert the consumer's `go.mod` and `go.sum` afterwards.
   Use the consumer build instead of `golang.org/x/exp/cmd/apidiff`. apidiff recurses without end on generic methods such as `Option[T].Map[R any]` in `pkg/functional`: one run used 139 GB of memory before it was killed. See the [decision log](../decision-log.md).
2. Merge an approved change and select an unused semantic version for each module that changed. Tag them leaves first (`pkg/functional/vX.Y.Z`, then `db/vX.Y.Z`, then `vX.Y.Z`) on a `release/vX.Y.Z` branch with the steps in [RELEASING.md](../../RELEASING.md#release-steps); `main` keeps the development form of the `go.mod` files. Verify the fetched `origin/main` commit and the existing tags before you create a tag. A pushed tag is immutable.
3. In a consumer module, run `go get github.com/nanostack-dev/nanostack-framework@vX.Y.Z` and `go mod tidy`; review both module files. `go mod tidy` also brings the `db` and `pkg/functional` versions that the root requires; upgrade them together with the root (see [RELEASING.md](../../RELEASING.md#upgrade-from-v0220)). Remove temporary local replaces when verifying the published dependency.
4. Run the consumer's affected lint, integration/contract suites and build. Coordinate API migrations across framework packages and Fx modules so type identities agree.
5. Deploy the application through its runbook and verify its health and the adopted behavior. A framework tag alone is not deployment evidence.

Existing versions remain immutable. Document breaking changes and migration steps in the owning capability guide and companion consumer PRs. Private-module access and publication permissions remain repository/account concerns, not shared-workspace prerequisites.
