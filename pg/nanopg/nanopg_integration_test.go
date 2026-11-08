//go:build integration

package nanopg_test

import (
	"context"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/pg/internal/pgtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
)

// Only a real server shows that the pool connects, that a query runs through
// the transactor and is traced only under a parent span, and that Stop
// closes the pool. The server starts in a container (see pg/internal/pgtest):
//
//	go test -tags=integration ./pg/nanopg/

func config(db pgtest.Database) nanopg.Config {
	return nanopg.Config{
		Host: db.Host, Port: db.Port, User: db.User, Password: secret.New(db.Password), DB: db.Name,
		SSLMode: "disable", MaxOpenConns: 25, MaxIdleConns: 25,
	}
}

func TestNewDBConnectsQueriesAndCloses(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	db, err := nanopg.NewDB(t.Context(), config(pgtest.Fresh(t)), slog.New(slog.DiscardHandler), provider)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CheckHealth(t.Context()); err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}

	tx := nanopg.NewTransactor(nanopg.SQL(db))
	var one int
	if err = tx.InTx(t.Context(), func(ctx context.Context) error {
		rows, queryErr := transactor.Executor(ctx, nanopg.SQL(db)).QueryContext(ctx, "SELECT 1")
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		for rows.Next() {
			if scanErr := rows.Scan(&one); scanErr != nil {
				return scanErr
			}
		}
		return rows.Err()
	}); err != nil || one != 1 {
		t.Fatalf("query through the transactor = (%d, %v), want (1, nil)", one, err)
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Fatalf("a query without a parent span made %d spans, want 0", len(spans))
	}

	ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
	if _, err = nanopg.SQL(db).ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	parent.End()
	if spans := recorder.Ended(); len(spans) < 2 {
		t.Fatalf("a query under a parent span made %d spans, want the query span and its parent", len(spans))
	}

	if err = db.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err = db.CheckHealth(t.Context()); err == nil {
		t.Fatal("CheckHealth succeeded after Stop closed the pool")
	}
}
