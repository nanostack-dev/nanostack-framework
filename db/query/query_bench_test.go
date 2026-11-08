package query_test

import (
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/db/query"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// The scan benchmarks map 1000 rows of 8 columns, 4 of them nullable and NULL
// on every other row, into a struct, through the stdlib fake driver of
// fake_driver_test.go, so the rows measure qrm alone and not a network.
//
//   - impl=legacy scans into pointer fields with stmt.QueryContext, the call
//     pkg/db/transactor.Query makes (go-jet's upstream path).
//   - impl=beta-qrm scans into functional.Option fields with the same call.
//   - impl=beta is query.All into the Option struct, the beta API.
//
// Both structs are named Invoice, declared in their own function, so qrm maps
// the same "invoice.*" columns into each.

const benchRows = 1000

var invoiceColumns = []string{
	"invoice.id", "invoice.tenant_id", "invoice.number", "invoice.amount_cents",
	"invoice.created_at", "invoice.paid_at", "invoice.note", "invoice.external_ref",
}

func invoiceRows() [][]driver.Value {
	created := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)
	rows := make([][]driver.Value, benchRows)
	for i := range rows {
		row := []driver.Value{
			int64(i + 1), "ten_42", fmt.Sprintf("INV-%06d", i), int64(1000 + i),
			created.Add(time.Duration(i) * time.Minute), nil, nil, nil,
		}
		if i%2 == 0 {
			row[5] = created.Add(time.Duration(i) * time.Hour)
			row[6] = "paid by card"
			row[7] = fmt.Sprintf("ch_%08d", i)
		}
		rows[i] = row
	}
	return rows
}

func BenchmarkScan(b *testing.B) {
	db := openFake(script{columns: invoiceColumns, rows: invoiceRows()})
	b.Cleanup(func() { _ = db.Close() })

	b.Run("impl=legacy/rows=1000", func(b *testing.B) {
		type Invoice struct {
			ID          int64 `sql:"primary_key"`
			TenantID    string
			Number      string
			AmountCents int64
			CreatedAt   time.Time
			PaidAt      *time.Time
			Note        *string
			ExternalRef *string
		}
		b.ReportAllocs()
		for b.Loop() {
			var got []Invoice
			if err := stmt.QueryContext(ctx, db, &got); err != nil {
				b.Fatal(err)
			}
			if len(got) != benchRows || got[0].PaidAt == nil || got[1].PaidAt != nil {
				b.Fatalf("mapped %d rows wrongly", len(got))
			}
		}
	})

	type Invoice struct {
		ID          int64 `sql:"primary_key"`
		TenantID    string
		Number      string
		AmountCents int64
		CreatedAt   time.Time
		PaidAt      functional.Option[time.Time]
		Note        functional.Option[string]
		ExternalRef functional.Option[string]
	}

	b.Run("impl=beta-qrm/rows=1000", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var got []Invoice
			if err := stmt.QueryContext(ctx, db, &got); err != nil {
				b.Fatal(err)
			}
			if len(got) != benchRows || !got[0].PaidAt.IsPresent() || got[1].PaidAt.IsPresent() {
				b.Fatalf("mapped %d rows wrongly", len(got))
			}
		}
	})

	b.Run("impl=beta/rows=1000", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			got, err := query.All[Invoice](ctx, db, stmt)
			if err != nil {
				b.Fatal(err)
			}
			if len(got) != benchRows || !got[0].PaidAt.IsPresent() || got[1].PaidAt.IsPresent() {
				b.Fatalf("mapped %d rows wrongly", len(got))
			}
		}
	})
}
