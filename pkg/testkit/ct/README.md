# ct

Contract-test harness primitives.

This package intentionally stops at generic setup concerns:

- container specs for Postgres, Redis, and WireMock
- environment injection
- dynamic port allocation
- health polling
- generated client hook points

Product fixtures, repositories, stubs, and fluent scenario builders stay in app test packages.
