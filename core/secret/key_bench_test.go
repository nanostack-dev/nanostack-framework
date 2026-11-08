package secret_test

import (
	"net/url"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// sinkBool keeps the compiler from dropping a call whose result is unused.
var sinkBool bool //nolint:gochecknoglobals // benchmark sink.

// BenchmarkIsKey measures the key guard core/log runs on every attribute key
// of every record, and on every query parameter of the request summary. A
// miss walks the whole suffix table, so it is the common and the dearest
// case.
func BenchmarkIsKey(b *testing.B) {
	for _, c := range []struct {
		name, key string
		want      bool
	}{
		{"key=hit-short", "token", true},
		{"key=hit-header", "Authorization", true},
		{"key=hit-late", "database_url", true},
		{"key=miss-short", "status", false},
		{"key=miss-typical", "request_id", false},
		{"key=miss-long", "http.server.request.header.x_forwarded_for_original_client", false},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkBool = secret.IsKey(c.key)
			}
			if sinkBool != c.want {
				b.Fatalf("IsKey(%q) = %v, want %v", c.key, sinkBool, c.want)
			}
		})
	}
}

// BenchmarkRedactQuery measures the query redaction of a DSN or a callback
// URL, with and without a credential to replace.
func BenchmarkRedactQuery(b *testing.B) {
	for _, c := range []struct{ name, query string }{
		{"query=clean", "limit=20&cursor=abc&sort=created_at&order=desc"},
		{"query=secret", "limit=20&cursor=abc&api_key=sk_live_0123456789&order=desc"},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = secret.RedactQuery(c.query)
			}
		})
	}
	b.Run("url=dsn", func(b *testing.B) {
		u, err := url.Parse("postgres://app:pw@db.internal:5432/app?sslmode=require&sslpassword=pw2")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			_ = secret.RedactURL(u)
		}
	})
}
