# Testkit

The framework testkit should extract only generic test harness infrastructure.

## Owns

- Postgres, Redis, and WireMock container specs
- environment variable injection
- dynamic port allocation
- app startup callbacks
- health polling
- generated client factory hooks
- OpenAPI parsing and route enumeration helpers

## Does Not Own

- product fixture repositories
- seeded domain objects
- organization/user/API-key scenario builders
- generated client route calls
- product-specific WireMock stubs

Those stay in `anchor` and `echopoint` test packages so product behavior remains explicit.
