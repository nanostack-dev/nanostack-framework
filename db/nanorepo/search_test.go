package nanorepo_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/nanorepo"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// The tables of the declarations below: the webhooks of an organization and
// a workspace, the requests each webhook received, and the tags of a request.
var (
	webhookID          = postgres.StringColumn("id")
	webhookOrgID       = postgres.StringColumn("organization_id")
	webhookWorkspaceID = postgres.StringColumn("workspace_id")
	webhookTemplateOf  = postgres.StringColumn("template_organization_id")
	webhookName        = postgres.StringColumn("name")
	webhookStatus      = postgres.StringColumn("status")
	webhookCreatedAt   = postgres.TimestampzColumn("created_at")
	webhooks           = postgres.NewTable("public", "webhooks", "",
		webhookID, webhookOrgID, webhookWorkspaceID, webhookTemplateOf, webhookName, webhookStatus, webhookCreatedAt)

	requestID         = postgres.StringColumn("id")
	requestOrgID      = postgres.StringColumn("organization_id")
	requestWebhookID  = postgres.StringColumn("webhook_id")
	requestMethod     = postgres.StringColumn("method")
	requestIPAddress  = postgres.StringColumn("ip_address")
	requestStatus     = postgres.StringColumn("status")
	requestReceivedAt = postgres.TimestampzColumn("received_at")
	requestDeletedAt  = postgres.TimestampzColumn("deleted_at")
	requests          = postgres.NewTable("public", "webhook_requests", "",
		requestID, requestOrgID, requestWebhookID, requestMethod, requestIPAddress, requestStatus,
		requestReceivedAt, requestDeletedAt)

	tagRequestID = postgres.StringColumn("request_id")
	tagName      = postgres.StringColumn("name")
	requestTags  = postgres.NewTable("public", "webhook_request_tags", "", tagRequestID, tagName)
)

// webhookRequest is the generated model of webhook_requests.
type webhookRequest struct {
	ID             string
	OrganizationID string
	WebhookID      string
	Method         string
	IPAddress      string
	Status         string
	ReceivedAt     time.Time
	DeletedAt      *time.Time
}

// webhookRow is a webhook as a search reads it, with a computed count.
type webhookRow struct {
	ID           string
	Name         string
	RequestCount int64
}

type requestDomain struct{ ID string }

type webhookDomain struct{ ID string }

// requestStatusValue is an enum over the status column.
type requestStatusValue string

// requestScope is the scope type that nanostack gen repo generates for a search.
type requestScope struct{ OrganizationID string }

// searchBody is a request body type of the api step.
type searchBody struct{}

type requestMapper struct{}

func (requestMapper) RequestToDomain(r webhookRequest) requestDomain { return requestDomain{ID: r.ID} }

type searchRepositoryImpl struct{}

func (*searchRepositoryImpl) webhooksToDomain(_ context.Context, rows []webhookRow) ([]webhookDomain, error) {
	out := make([]webhookDomain, len(rows))
	for i, r := range rows {
		out[i] = webhookDomain{ID: r.ID}
	}
	return out, nil
}

func requestToDomain(r webhookRequest) requestDomain { return requestDomain{ID: r.ID} }

// workspaceFilter stands for the context scope function that project.go
// registers for workspace_id.
func workspaceFilter(_ context.Context, column postgres.ColumnString) postgres.BoolExpression {
	return column.EQ(postgres.String("w1"))
}

func requestsOfWebhooks() postgres.ReadableTable {
	return requests.INNER_JOIN(webhooks, webhookID.EQ(requestWebhookID))
}

func requestsInOrganization(s requestScope) postgres.ReadableTable {
	return requests.LEFT_JOIN(webhooks, webhookID.EQ(requestWebhookID).
		AND(webhookOrgID.EQ(postgres.String(s.OrganizationID))))
}

func standaloneWebhooks() postgres.BoolExpression {
	return webhookStatus.EQ(postgres.String("standalone"))
}

func requestHost() postgres.StringExpression { return postgres.LOWER(requestIPAddress) }

func requestHasQueryParams() postgres.BoolExpression {
	return requestMethod.EQ(postgres.String("GET"))
}

func requestCount() postgres.IntegerExpression {
	return postgres.IntExp(postgres.SELECT(postgres.COUNT(requestID)).
		FROM(requests).
		WHERE(requestWebhookID.EQ(webhookID).AND(requestOrgID.EQ(webhookOrgID))))
}

// Every option of a search declaration compiles in func _(), the place
// nanostack gen repo reads declarations from.
func _() {
	nanorepo.Search[webhookRequest](
		"WebhookRequest",
		requests,
		nanorepo.Map(requestToDomain),
		nanorepo.From(requestsOfWebhooks),
		nanorepo.Scope(requestOrgID, webhookOrgID),
		nanorepo.ScopeFunc(workspaceFilter, webhookWorkspaceID),
		nanorepo.Condition(standaloneWebhooks),
		nanorepo.Filter(requestMethod, nanorepo.Eq, nanorepo.In.API("methods"), nanorepo.InFold),
		nanorepo.FilterAs[requestStatusValue](requestStatus, nanorepo.Eq, nanorepo.In),
		nanorepo.FilterExpr("host", requestHost, nanorepo.Contains, nanorepo.ContainsAny.API("hosts")),
		nanorepo.FilterExists(requestTags, tagRequestID, tagName, nanorepo.In.API("tags")),
		nanorepo.Filter(requestReceivedAt, nanorepo.Range("received_since", "received_until").HalfOpen()),
		nanorepo.Filter(
			requestDeletedAt,
			nanorepo.Present,
			nanorepo.Range("deleted_after", "deleted_before").Exclusive(),
		),
		nanorepo.Flag("HasQueryParams", requestHasQueryParams),
		nanorepo.GoOnly(nanorepo.Filter(requestWebhookID, nanorepo.Eq)),
		nanorepo.GoOnly(nanorepo.FilterAs[requestStatusValue](requestStatus, nanorepo.In)),
		nanorepo.FullText(requestMethod, requestIPAddress),
		nanorepo.FullTextExpr(requestHost),
		nanorepo.Sort(requestReceivedAt, requestMethod),
		nanorepo.SortKey(requestStatus).API("status", "state").NullsLast(),
		nanorepo.SortKey(requestDeletedAt).NullsFirst(),
		nanorepo.Tiebreak(requestID),
		nanorepo.DefaultSort(requestReceivedAt, nanorepo.Desc),
		nanorepo.DefaultLimit(20),
		nanorepo.API[searchBody](),
		nanorepo.APIHandWritten("folder_id", "include_nested"),
	)

	nanorepo.Search[webhookRequest]("OrganizationRequest", requests,
		nanorepo.Map((*requestMapper).RequestToDomain),
		nanorepo.From(requestsInOrganization),
		nanorepo.Scope(requestOrgID),
		nanorepo.Unscoped(webhookWorkspaceID, "an organization admin reads every workspace"),
		nanorepo.Filter(requestReceivedAt, nanorepo.Range("since")),
		nanorepo.Sort(requestReceivedAt),
		nanorepo.DefaultSort(requestReceivedAt, nanorepo.Asc),
		nanorepo.API[searchBody](),
		nanorepo.IgnoresFullText("the product has not chosen the columns yet"),
	)

	// The shared-row scopes only show that the options compile: webhooks has
	// no shared rows, and template_organization_id is not a tenant column.
	nanorepo.Search[webhookRow]("Webhook", webhooks,
		nanorepo.MapPage((*searchRepositoryImpl).webhooksToDomain),
		nanorepo.ScopeOrNull(webhookOrgID),
		nanorepo.ScopeNull(webhookTemplateOf),
		nanorepo.ScopeFunc(workspaceFilter, webhookWorkspaceID),
		nanorepo.Sort(webhookName),
		nanorepo.SortExpr("RequestCount", requestCount),
		nanorepo.SortHandWritten("flow_count"),
		nanorepo.DefaultSortExpr("RequestCount", nanorepo.Desc),
	)
}

// The declarations are dialect-neutral: tables, columns and conditions of
// MySQL and SQLite compile as PostgreSQL's do. In this release, nanostack gen repo
// refuses these two searches (E16).
func _() {
	mysqlID := mysql.StringColumn("id")
	mysqlOrgID := mysql.StringColumn("organization_id")
	mysqlItems := mysql.NewTable("app", "items", "", mysqlID, mysqlOrgID)
	nanorepo.Search[struct{ ID string }]("Item", mysqlItems,
		nanorepo.Scope(mysqlOrgID),
		nanorepo.Condition(func() mysql.BoolExpression { return mysqlID.IS_NOT_NULL() }),
		nanorepo.Sort(mysqlID),
	)

	sqliteID := sqlite.StringColumn("id")
	sqliteWorkspaceID := sqlite.StringColumn("workspace_id")
	sqliteItems := sqlite.NewTable("", "items", "", sqliteID, sqliteWorkspaceID)
	nanorepo.Search[struct{ ID string }]("Item", sqliteItems,
		nanorepo.ScopeFunc(func(context.Context, sqlite.ColumnString) sqlite.BoolExpression {
			return sqlite.Bool(true)
		}, sqliteWorkspaceID),
		nanorepo.Sort(sqliteID),
	)
}

// SortSpec is a SearchOption, so a SortKey goes straight into Search.
var _ nanorepo.SearchOption = nanorepo.SortKey(webhookName).API("name")

// The declarations do nothing when Go calls them.
func TestSearchDeclarationsDoNothing(t *testing.T) {
	t.Parallel()
	nanorepo.Search[webhookRequest]("WebhookRequest", requests,
		nanorepo.Map(requestToDomain),
		nanorepo.From(requestsInOrganization),
		nanorepo.Scope(requestOrgID),
		nanorepo.Filter(requestMethod, nanorepo.Eq, nanorepo.In),
		nanorepo.SortKey(requestStatus).API("status").NullsLast(),
		nanorepo.Tiebreak(requestID),
	)
	ops := []nanorepo.Op{
		nanorepo.Eq, nanorepo.In, nanorepo.InFold, nanorepo.Contains, nanorepo.ContainsAny, nanorepo.Present,
		nanorepo.Range("since").HalfOpen().Exclusive().API("after", "before"),
	}
	if len(ops) != 7 {
		t.Fatalf("len(ops) = %d, want 7", len(ops))
	}
	if nanorepo.Asc == nanorepo.Desc {
		t.Fatal("Asc and Desc are the same direction")
	}
}

// A search option is not a repository option, and Search takes only search
// options. Each package under testdata except implementok makes one of these
// mistakes and must not compile. implementok is the same code as the
// Implement cases without the mistake, and compiles.
func TestSearchOptionTypeGuard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dir  string
		want []string // nil: the package compiles
	}{
		{dir: "./testdata/implementok"},
		{dir: "./testdata/implementextraoption", want: []string{
			"nanorepo.SearchOption does not implement nanorepo.Option",
		}},
		{dir: "./testdata/implementoptionasmapper", want: []string{"nanorepo.SearchOption"}},
		{dir: "./testdata/searchforeignoption", want: []string{
			"forged does not implement nanorepo.SearchOption",
			"nanorepo.Op does not implement nanorepo.SearchOption",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			out, err := exec.CommandContext(t.Context(), "go", "build", tc.dir).CombinedOutput()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("go build %s: %v\n%s", tc.dir, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("go build %s succeeded, want a compile error", tc.dir)
			}
			got := string(out)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("go build %s:\n%s\nwant an error with %q", tc.dir, got, want)
				}
			}
		})
	}
}
