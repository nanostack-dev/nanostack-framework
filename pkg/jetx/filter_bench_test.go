package jetx_test

import (
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/filter"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"github.com/nanostack-dev/nanostack-framework/pkg/jetx"
)

// The SQL build benchmarks render a search SELECT with a tenant key and 5
// optional filters (status, created-at range, amount floor, ids, name
// substring), every filter present, and call Sql() as QueryContext does.
//
//   - impl=legacy is the v0.22 shape: pointer and slice inputs, the jetx
//     Build* helpers and CombineFilters, plain value literals.
//   - impl=beta is the beta shape: functional.Option inputs, db/filter
//     When/If/All, plain value literals inside the closures.
//   - impl=beta-optlit is impl=beta with the XxxOpt literals (StringOpt,
//     IntOpt, TimestampzOpt) taking the Option directly, guarded by If.
//
// The file sits in the legacy tree because only a legacy package may import
// pkg/jetx next to db/filter.

var (
	benchID       = jet.StringColumn("id")
	benchTenant   = jet.StringColumn("tenant_id")
	benchStatus   = jet.StringColumn("status")
	benchCreated  = jet.TimestampzColumn("created_at")
	benchAmount   = jet.IntegerColumn("amount_cents")
	benchName     = jet.StringColumn("name")
	benchInvoices = jet.NewTable("billing", "invoice", "",
		benchID, benchTenant, benchStatus, benchCreated, benchAmount, benchName)
)

type legacySearch struct {
	Tenant    string
	Status    *string
	From, To  *time.Time
	MinAmount *int64
	IDs       []string
	Term      string
}

type betaSearch struct {
	Tenant    string
	Status    functional.Option[string]
	From, To  functional.Option[time.Time]
	MinAmount functional.Option[int64]
	IDs       []string
	Term      functional.Option[string]
}

func benchStatement(where jet.BoolExpression) jet.SelectStatement {
	return jet.SELECT(benchID, benchTenant, benchStatus, benchCreated, benchAmount, benchName).
		FROM(benchInvoices).
		WHERE(where).
		ORDER_BY(benchCreated.DESC(), benchID.DESC()).
		LIMIT(50)
}

func legacyWhere(s legacySearch) jet.BoolExpression {
	var status, amount jet.BoolExpression
	if s.Status != nil {
		status = benchStatus.EQ(jet.String(*s.Status))
	}
	if s.MinAmount != nil {
		amount = benchAmount.GT_EQ(jet.Int(*s.MinAmount))
	}
	return jetx.CombineFilters(
		benchTenant.EQ(jet.String(s.Tenant)),
		status,
		jetx.BuildDateRangeFilter(benchCreated, s.From, s.To),
		amount,
		jetx.BuildIDFilter(benchID, s.IDs),
		jetx.BuildSubstringFilter([]jet.ColumnString{benchName}, s.Term),
	)
}

func betaWhere(s betaSearch) jet.BoolExpression {
	return filter.All(
		benchTenant.EQ(jet.String(s.Tenant)),
		filter.When(s.Status, func(v string) jet.BoolExpression { return benchStatus.EQ(jet.String(v)) }),
		filter.When(s.From, func(t time.Time) jet.BoolExpression { return benchCreated.GT_EQ(jet.TimestampzT(t)) }),
		filter.When(s.To, func(t time.Time) jet.BoolExpression { return benchCreated.LT_EQ(jet.TimestampzT(t)) }),
		filter.When(s.MinAmount, func(v int64) jet.BoolExpression { return benchAmount.GT_EQ(jet.Int(v)) }),
		filter.If(len(s.IDs) > 0, func() jet.BoolExpression { return benchID.IN(jetx.ToStringExpressions(s.IDs)...) }),
		filter.When(s.Term, func(v string) jet.BoolExpression { return benchName.LIKE(jet.String("%" + v + "%")) }),
	)
}

func betaOptLiteralWhere(s betaSearch) jet.BoolExpression {
	return filter.All(
		benchTenant.EQ(jet.String(s.Tenant)),
		filter.If(s.Status.IsPresent(), func() jet.BoolExpression { return benchStatus.EQ(jet.StringOpt(s.Status)) }),
		filter.If(
			s.From.IsPresent(),
			func() jet.BoolExpression { return benchCreated.GT_EQ(jet.TimestampzOpt(s.From)) },
		),
		filter.If(s.To.IsPresent(), func() jet.BoolExpression { return benchCreated.LT_EQ(jet.TimestampzOpt(s.To)) }),
		filter.If(
			s.MinAmount.IsPresent(),
			func() jet.BoolExpression { return benchAmount.GT_EQ(jet.IntOpt(s.MinAmount)) },
		),
		filter.If(len(s.IDs) > 0, func() jet.BoolExpression { return benchID.IN(jetx.ToStringExpressions(s.IDs)...) }),
		filter.When(s.Term, func(v string) jet.BoolExpression { return benchName.LIKE(jet.String("%" + v + "%")) }),
	)
}

func BenchmarkSearchSQL(b *testing.B) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	status, minAmount := "open", int64(5000)
	ids := []string{"inv_1", "inv_2", "inv_3"}
	legacy := legacySearch{Tenant: "ten_42", Status: &status, From: &from, To: &to, MinAmount: &minAmount,
		IDs: ids, Term: "acme"}
	beta := betaSearch{
		Tenant:    "ten_42",
		Status:    functional.Some(status),
		From:      functional.Some(from),
		To:        functional.Some(to),
		MinAmount: functional.Some(minAmount),
		IDs:       ids,
		Term:      functional.Some("acme"),
	}
	const wantArgs = 10 // tenant, status, from, to, amount, 3 ids, term, limit

	builds := []struct {
		name  string
		where func() jet.BoolExpression
	}{
		{"impl=legacy", func() jet.BoolExpression { return legacyWhere(legacy) }},
		{"impl=beta", func() jet.BoolExpression { return betaWhere(beta) }},
		{"impl=beta-optlit", func() jet.BoolExpression { return betaOptLiteralWhere(beta) }},
	}
	for _, build := range builds {
		b.Run(build.name+"/filters=5", func(b *testing.B) {
			b.ReportAllocs()
			var args []any
			for b.Loop() {
				_, args = benchStatement(build.where()).Sql()
			}
			if len(args) != wantArgs {
				b.Fatalf("args = %d, want %d", len(args), wantArgs)
			}
		})
	}
}
