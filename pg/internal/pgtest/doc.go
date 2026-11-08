// Package pgtest gives the integration tests of the pg modules a real
// PostgreSQL server.
//
// The server starts on first use and is shared by every test of the test
// binary. Each test gets a database of its own, dropped when the test ends,
// so tests can run in parallel against the one server.
//
// Set PG_TEST_DSN to use a server you run yourself. Otherwise the package
// starts a container (PG_TEST_IMAGE, default DefaultImage) through
// testcontainers, which removes it when the test binary exits.
package pgtest
