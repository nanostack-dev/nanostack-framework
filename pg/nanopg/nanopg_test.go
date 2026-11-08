package nanopg_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/lib/pq"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
)

func TestDSNQuotesEveryValue(t *testing.T) {
	cfg := nanopg.Config{
		Host: "db.internal", Port: 5432, User: "app", DB: "billing",
		Password: secret.New(`it's a "pass" \ word`), SSLMode: "verify-full",
	}

	dsn := cfg.DSN()

	want := `host='db.internal' port=5432 user='app' dbname='billing' ` +
		`password='it\'s a "pass" \\ word' sslmode='verify-full'`
	if dsn != want {
		t.Fatalf("DSN() =\n%s\nwant\n%s", dsn, want)
	}
	if _, err := pq.NewConnector(dsn); err != nil {
		t.Fatalf("lib/pq rejects the DSN: %v", err)
	}
}

func TestDSNLeavesOutAnEmptyPasswordAndSSLMode(t *testing.T) {
	dsn := nanopg.Config{Host: "h", Port: 1, User: "u", DB: "d"}.DSN()
	if strings.Contains(dsn, "password") || strings.Contains(dsn, "sslmode") {
		t.Fatalf("DSN() = %q, want no password and no sslmode", dsn)
	}
}

func TestConfigNeverPrintsThePassword(t *testing.T) {
	cfg := nanopg.Config{Password: secret.New("hunter2")}
	if text := slog.AnyValue(cfg).String(); strings.Contains(text, "hunter2") {
		t.Fatalf("Config printed its password: %s", text)
	}
}

func TestNewDBFailsWhenTheServerIsUnreachable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Port 1 refuses the connection at once.
	_, err := nanopg.NewDB(ctx, nanopg.Config{
		Host: "127.0.0.1", Port: 1, User: "u", DB: "d", SSLMode: "disable", MaxOpenConns: 1,
	}, slog.New(slog.DiscardHandler), noop.NewTracerProvider())
	if err == nil {
		t.Fatal("NewDB succeeded against a closed port")
	}
	if !strings.Contains(err.Error(), "nanopg: ping 127.0.0.1:1") {
		t.Fatalf("err = %v, want the ping failure with the address", err)
	}
}
