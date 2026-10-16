//go:build integration

package searchtest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"

	"github.com/nanostack-dev/nanostack-framework/modules/httpserver"
	"github.com/nanostack-dev/nanostack-framework/pkg/db/transactor"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
	"github.com/nanostack-dev/nanostack-framework/pkg/testkit/searchtest"
)

// The integration test runs the kit against a real search: a Postgres table,
// a PageBuilder with StrictKeys, and the framework's StrictErrorHandler over
// HTTP. Forged cursors for every key type must never produce a 5xx.
//
//	PGERR_TEST_DSN="postgres://postgres:itpass@localhost:55432/pgerrit?sslmode=disable" \
//		go test -tags=integration ./pkg/testkit/searchtest/

type field string

type row struct {
	ID      string    `alias:"it_searchtest.id"`
	Tenant  string    `alias:"it_searchtest.tenant"`
	Name    string    `alias:"it_searchtest.name"`
	Status  string    `alias:"it_searchtest.status"`
	Created time.Time `alias:"it_searchtest.created"`
	Score   *float64  `alias:"it_searchtest.score"`
}

var (
	colID      = jet.StringColumn("id")
	colTenant  = jet.StringColumn("tenant")
	colName    = jet.StringColumn("name")
	colStatus  = jet.StringColumn("status")
	colCreated = jet.TimestampzColumn("created")
	colScore   = jet.FloatColumn("score")
	table      = jet.NewTable("public", "it_searchtest", "", colID, colTenant, colName, colStatus, colCreated, colScore)
)

func keys() map[field]transactor.Key[row] {
	return map[field]transactor.Key[row]{
		"created_at": transactor.TimeKey(colCreated, func(r row) time.Time { return r.Created }),
		"name":       transactor.StringKey(colName, func(r row) string { return r.Name }),
		"status": transactor.EnumKey(colStatus, "it_searchtest_status", []string{"pending", "active", "archived"},
			func(r row) string { return r.Status }),
		"score": transactor.NullableFloatKey(colScore, func(r row) *float64 { return r.Score }),
	}
}

func handler(db *sql.DB) http.Handler {
	errs := httpserver.NewStrictErrorHandler(httpserver.StrictErrorHandlerOptions{Logger: zerolog.Nop()})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tenant        string                `json:"tenant"`
			SortBy        *field                `json:"sort_by"`
			SortDirection *search.SortDirection `json:"sort_direction"`
			Pagination    *search.Pagination    `json:"pagination"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errs.HandleRequestError(w, r, err)
			return
		}
		req := search.NewRequest[struct{}, field]().WithSort(body.SortBy, body.SortDirection).WithPagination(body.Pagination)
		order, err := transactor.StrictKeys(req.Sort, keys(), transactor.StringKey(colID, func(r row) string { return r.ID }))
		if err != nil {
			errs.HandleResponseError(w, r, err)
			return
		}
		page, err := transactor.Page(db, func(r row) map[string]any { return map[string]any{"id": r.ID} },
			colID, colTenant, colName, colStatus, colCreated, colScore).
			From(table).Where(colTenant.EQ(jet.String(body.Tenant))).Keyset(order).
			Run(r.Context(), req.Pagination).Value()
		if err != nil {
			errs.HandleResponseError(w, r, err)
			return
		}
		response := map[string]any{"items": page.Items, "total": page.Total, "count": page.Count}
		if page.NextCursor != "" {
			response["next_cursor"] = page.NextCursor
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
}

func setup(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PGERR_TEST_DSN")
	if dsn == "" {
		t.Skip("PGERR_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	created := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	nan := math.NaN()
	one, two := 1.0, 2.0
	type seed struct {
		id, name, status string
		score            *float64
	}
	// Names, statuses and scores are assigned out of id order, with ties, so
	// each sort orders the rows differently from the others and from the id.
	seeds := []seed{
		{"w1", "c", "active", &two}, {"w2", "a", "pending", nil}, {"w3", "b", "archived", &one},
		{"w4", "a", "active", nil}, {"w5", "c", "pending", &nan}, {"w6", "b", "archived", &two},
		{"w7", "a", "active", nil},
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS it_searchtest`,
		`DROP TYPE IF EXISTS it_searchtest_status`,
		`CREATE TYPE it_searchtest_status AS ENUM ('pending', 'active', 'archived')`,
		`CREATE TABLE it_searchtest (id text PRIMARY KEY, tenant text NOT NULL, name varchar(255) NOT NULL,
			status it_searchtest_status NOT NULL, created timestamptz NOT NULL, score double precision)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS it_searchtest`)
		_, _ = db.Exec(`DROP TYPE IF EXISTS it_searchtest_status`)
	})
	for _, s := range seeds {
		if _, err := db.Exec(`INSERT INTO it_searchtest VALUES ($1, 't1', $2, $3, $4, $5)`,
			s.id, s.name, s.status, created, s.score); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestIntegrationConformanceOfARealSearch(t *testing.T) {
	server := httptest.NewServer(handler(setup(t)))
	t.Cleanup(server.Close)
	call := func(ctx context.Context, body map[string]any) (int, []byte, error) {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(string(encoded)))
		if err != nil {
			return 0, nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		response, err := io.ReadAll(resp.Body)
		return resp.StatusCode, response, err
	}
	t1 := func(extra map[string]any) map[string]any {
		body := map[string]any{"tenant": "t1"}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	searchtest.Run(t, searchtest.Suite{
		Name: "it_searchtest",
		Call: call,
		Cases: []searchtest.Case{
			// Every created timestamp ties, so the default order is the id.
			{Name: "default", Body: t1(nil), Want: []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7"}},
			{Name: "created desc", Body: t1(map[string]any{"sort_by": "created_at", "sort_direction": "DESC"}),
				Want: []string{"w7", "w6", "w5", "w4", "w3", "w2", "w1"}},
			{Name: "name asc", Body: t1(map[string]any{"sort_by": "name", "sort_direction": "ASC"}),
				Want: []string{"w2", "w4", "w7", "w3", "w6", "w1", "w5"}},
			{Name: "name desc", Body: t1(map[string]any{"sort_by": "name", "sort_direction": "DESC"}),
				Want: []string{"w5", "w1", "w6", "w3", "w7", "w4", "w2"}},
			// Enum declaration order, not text order.
			{Name: "status", Body: t1(map[string]any{"sort_by": "status", "sort_direction": "ASC"}),
				Want: []string{"w2", "w5", "w1", "w4", "w7", "w3", "w6"}},
			// NaN sorts above every number; NULLs last when ascending.
			{Name: "score", Body: t1(map[string]any{"sort_by": "score", "sort_direction": "ASC"}),
				Want: []string{"w3", "w1", "w6", "w5", "w2", "w4", "w7"}},
		},
		Refusals: searchtest.Refusals{
			Scopes: []map[string]any{
				t1(nil),
				t1(map[string]any{"sort_by": "name"}),
				t1(map[string]any{"sort_by": "status"}),
				t1(map[string]any{"sort_by": "score", "sort_direction": "DESC"}),
				{"tenant": "nobody"},
			},
			SortField:    true,
			Incompatible: t1(map[string]any{"sort_by": "name"}),
		},
		Mutate: searchtest.FrameworkCursorMutations,
	})
}
