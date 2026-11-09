package keyset_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/search"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// The fixture of the dialect tests: one widgets table in each dialect package, a key per
// key constructor, and the computed keys over the widget's requests.

type dRow struct {
	ID      string
	Name    string
	Note    *string
	Status  string
	StatusP *string
	Ref     uuid.UUID
	Rank    int64
	Score   *float64
	Ratio   float32
	Created time.Time
	Stamp   time.Time
	At      time.Time
	IP      string
	// The computed keys: aggregates of the widget's requests.
	Requests int64
	Biggest  *int64
	Total    float64
	Label    *string
	Last     time.Time
}

type dField string

const (
	dName    dField = "NAME"
	dNote    dField = "NOTE"
	dNoteNF  dField = "NOTE_NULLS_FIRST"
	dNoteNL  dField = "NOTE_NULLS_LAST"
	dStatus  dField = "STATUS"
	dStatusP dField = "STATUS_NULLABLE"
	dRef     dField = "REF"
	dRank    dField = "RANK"
	dScore   dField = "SCORE"
	dRatio   dField = "RATIO"
	dCreated dField = "CREATED"
	dStamp   dField = "STAMP"
	dAt      dField = "AT"
	dIP      dField = "IP"
	// dCreatedSQL is CREATED as SQL writes it on SQLite (CURRENT_TIMESTAMP).
	dCreatedSQL dField = "CREATED_SQL_TEXT"
	// dStampMaria is STAMP on MariaDB 11.5 and later.
	dStampMaria dField = "STAMP_MARIADB"
	// The computed keys: aggregates of the widget's requests.
	dRequests dField = "REQUEST_COUNT"
	dBiggest  dField = "BIGGEST"
	dTotal    dField = "TOTAL"
	dLabel    dField = "LABEL"
	dLast     dField = "LAST_RECEIVED"
)

var dLabels = []string{"pending", "active", "archived"}

// dTable is the same table in one dialect package.
type dTable struct {
	dialect string
	source  keyset.Source
	keys    map[dField]keyset.Key[dRow]
	id      keyset.Key[dRow]
}

var dDialects = []string{"postgres", "mysql", "sqlite"}

func newDTable(dialect string) dTable {
	var (
		id      = jet.StringColumn("id")
		name    = jet.StringColumn("name")
		note    = jet.StringColumn("note")
		status  = jet.StringColumn("status")
		statusP = jet.StringColumn("status_p")
		ref     = jet.StringColumn("ref")
		rank    = jet.IntegerColumn("rank")
		score   = jet.FloatColumn("score")
		ratio   = jet.FloatColumn("ratio")
		created = jet.TimestampColumn("created")
		stamp   = jet.TimestampColumn("stamp")
		at      = jet.TimestampzColumn("at")
		ip      = jet.StringColumn("ip")
	)
	table := dTable{dialect: dialect}
	ofWidget := dOfWidget(dialect, id)
	switch dialect {
	case "postgres":
		table.source = jet.NewTable("public", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
	case "mysql":
		table.source = mysql.NewTable("db", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
	case "sqlite":
		table.source = sqlite.NewTable("", "widgets", "",
			id, name, note, status, statusP, ref, rank, score, ratio, created, stamp, at, ip)
	}
	table.id = keyset.StringKey(id, func(r dRow) string { return r.ID })
	table.keys = map[dField]keyset.Key[dRow]{
		dName:    keyset.StringKey(name, func(r dRow) string { return r.Name }),
		dNote:    keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }),
		dNoteNF:  keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }).NullsFirst(),
		dNoteNL:  keyset.NullableStringKey(note, func(r dRow) *string { return r.Note }).NullsLast(),
		dStatus:  keyset.EnumKey(status, "widget_status", dLabels, func(r dRow) string { return r.Status }),
		dStatusP: keyset.NullableEnumKey(statusP, "widget_status", dLabels, func(r dRow) *string { return r.StatusP }),
		dRef:     keyset.UUIDKey(ref, func(r dRow) uuid.UUID { return r.Ref }),
		dRank:    keyset.IntKey(rank, func(r dRow) int64 { return r.Rank }),
		dScore:   keyset.NullableFloatKey(score, func(r dRow) *float64 { return r.Score }),
		dRatio:   keyset.RealKey(ratio, func(r dRow) float32 { return r.Ratio }),
		dCreated: keyset.LocalTimeKey(created, func(r dRow) time.Time { return r.Created }),
		dStamp:   keyset.MySQLTimestampKey(stamp, func(r dRow) time.Time { return r.Stamp }),
		dCreatedSQL: keyset.LocalTimeKey(created, func(r dRow) time.Time { return r.Created }).
			SQLiteLayout("2006-01-02 15:04:05"),
		dStampMaria: keyset.MariaDBTimestampKey(stamp, func(r dRow) time.Time { return r.Stamp }),
		dAt:         keyset.TimeKey(at, func(r dRow) time.Time { return r.At }),
		dIP:         keyset.InetKey(ip, func(r dRow) string { return r.IP }),
		dRequests: keyset.IntExprKey("widget_row.request_count", jet.IntExp(ofWidget(jet.COUNT(jet.STAR))),
			func(r dRow) int64 { return r.Requests }),
		dBiggest: keyset.NullableIntExprKey("widget_row.biggest", jet.IntExp(ofWidget(jet.MAXi(dReqSize))),
			func(r dRow) *int64 { return r.Biggest }),
		dTotal: keyset.FloatExprKey("widget_row.total", jet.FloatExp(ofWidget(jet.SUMf(dReqAmount))),
			func(r dRow) float64 { return r.Total }),
		dLabel: keyset.NullableStringExprKey("widget_row.label", jet.StringExp(ofWidget(jet.MIN(dReqLabel))),
			func(r dRow) *string { return r.Label }),
		dLast: keyset.TimeExprKey("widget_row.last_received", jet.TimestampzExp(ofWidget(jet.MAX(dReqReceived))),
			func(r dRow) time.Time { return r.Last }),
	}
	return table
}

func (d dTable) order(t *testing.T, sorts ...search.Sort[dField]) keyset.Order[dRow] {
	t.Helper()
	return mustOrder(t, sorts, d.keys, d.id)
}

func (d dTable) page() *keyset.PageBuilder[dRow, dRow] {
	return keyset.Page(nil, func(r dRow) dRow { return r }, jet.StringColumn("id")).From(d.source)
}

func asc(field dField) search.Sort[dField] {
	return search.Sort[dField]{Field: field, Direction: search.SortAscending}
}

func desc(field dField) search.Sort[dField] {
	return search.Sort[dField]{Field: field, Direction: search.SortDescending}
}

var dSample = dRow{
	ID:      "w_7",
	Name:    "apple",
	Note:    new("beta"),
	Status:  "active",
	StatusP: new("active"),
	Ref:     uuid.MustParse("018f6b8e-0000-7000-8000-000000000001"),
	Rank:    42,
	Score:   new(2.25),
	Ratio:   0.1,
	Created: time.Date(2026, time.March, 4, 10, 11, 12, 345_678_000, time.UTC),
	Stamp:   time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
	At:      time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
	IP:      "10.0.0.1",

	Requests: 3,
	Biggest:  new(int64(2)),
	Total:    1.25,
	Label:    new("b"),
	Last:     time.Date(2026, time.March, 4, 10, 11, 12, 0, time.UTC),
}

// The columns of the requests table, which the computed keys aggregate.
var (
	dReqWidget   = jet.StringColumn("widget_id")
	dReqSize     = jet.IntegerColumn("size")
	dReqAmount   = jet.FloatColumn("amount")
	dReqLabel    = jet.StringColumn("label")
	dReqReceived = jet.TimestampzColumn("received")
)

// dOfWidget returns a correlated subquery over the requests of the widget, in the builder of
// the dialect package.
func dOfWidget(dialect string, widgetID jet.ColumnString) func(jet.Projection) jet.Expression {
	columns := []jet.Column{dReqWidget, dReqSize, dReqAmount, dReqLabel, dReqReceived}
	where := dReqWidget.EQ(widgetID)
	switch dialect {
	case "mysql":
		requests := mysql.NewTable("db", "requests", "", columns...)
		return func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(where) }
	case "sqlite":
		requests := sqlite.NewTable("", "requests", "", columns...)
		return func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(where) }
	}
	requests := jet.NewTable("public", "requests", "", columns...)
	return func(p jet.Projection) jet.Expression { return requests.SELECT(p).WHERE(where) }
}
