//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/XSAM/otelsql"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Runs the real lib/pq driver through the otelsql wrapper with the options
// NewConnection uses.
func TestTracedQueriesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set")
	}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	db, err := otelsql.Open("postgres", dsn, traceOptions(provider)...)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	var one int
	if err := db.QueryRowContext(context.Background(), "select $1::int", 1).Scan(&one); err != nil {
		t.Fatal(err)
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("untraced query and ping produced %d spans", len(spans))
	}

	ctx, parent := provider.Tracer("test").Start(context.Background(), "request")
	if err := db.QueryRowContext(ctx, "select $1::int", 424242).Scan(&one); err != nil {
		t.Fatal(err)
	}
	parent.End()

	var query sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == string(otelsql.MethodConnQuery) {
			query = span
		}
		if span.Name() == string(otelsql.MethodRows) || span.Name() == string(otelsql.MethodConnResetSession) {
			t.Fatalf("unexpected noise span %q", span.Name())
		}
	}
	if query == nil {
		t.Fatalf("no %s span among %d", otelsql.MethodConnQuery, len(recorder.Ended()))
	}
	if query.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("query span is not a child of the request span")
	}
	for _, kv := range query.Attributes() {
		if strings.Contains(kv.Value.Emit(), "424242") {
			t.Fatalf("bind argument recorded in %s", kv.Key)
		}
	}
}
