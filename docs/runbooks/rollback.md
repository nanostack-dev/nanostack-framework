# Roll back a framework adoption

1. Identify the consumer's previous dependency version and any API, config or schema migration introduced by the new version.
2. Restore that compatible module version with `go get github.com/nanostack-dev/nanostack-framework@vPREVIOUS`, run `go mod tidy`, and revert the matching caller adaptations. Use the consumer's normal checks and deployment rollback procedure.
3. Verify application startup, dependency injection and the affected behavior. A dependency downgrade cannot undo database changes or completed external effects; use a reviewed forward fix where old code cannot consume current state.
4. Repair or revert the faulty framework source in a new PR and publish a new version. Preserve existing tags and record the verified cause in development or technical documentation.

For a significant incident, add a postmortem with prevention work. Coordinate framework and application fixes through linked PRs instead of assuming every consumer upgrades together.
