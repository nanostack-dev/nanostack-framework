// This file is Nanostack-only (not in go-jet): functional.Option models against SQLite.
// See db/UPSTREAM.md.

package sqlite

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	. "github.com/nanostack-dev/nanostack-framework/db/sqlite"
	optmodel "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/sqlite/test_sample/model"
	"github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/sqlite/test_sample/model"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/sqlite/test_sample/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestOptionScanParity(t *testing.T) {
	stmt := SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes)

	var opt []optmodel.AllTypes
	require.NoError(t, stmt.Query(sampleDB, &opt))
	var ptr []model.AllTypes
	require.NoError(t, stmt.Query(sampleDB, &ptr))

	require.Len(t, opt, len(ptr))
	require.GreaterOrEqual(t, len(opt), 2)
	for i := range opt {
		testutils.AssertOptionParity(t, opt[i], ptr[i])
	}
	// The test data has a row with every nullable column NULL and one with none.
	require.Positive(t, testutils.AssertOptionFields(t, opt[0], true))
	testutils.AssertOptionFields(t, opt[1], false)
}

// sqliteAllTypes returns a row with every NOT NULL column set and every Option absent.
func sqliteAllTypes(marker string) optmodel.AllTypes {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	return optmodel.AllTypes{
		Boolean: true, TinyInt: 1, SmallInt: 2, MediumInt: 3, Integer: 4, BigInt: 5,
		Decimal: decimal.RequireFromString("1.5"), Numeric: decimal.RequireFromString("2.5"),
		Float: 1.5, Double: 2.5, Real: 3.5,
		Time: time.Date(0, 1, 1, 3, 4, 5, 0, time.UTC), Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		DateTime: ts, Timestamp: ts,
		Char: "c", VarChar: "v", Text: marker, Blob: []byte("b"), JSON: `{}`,
	}
}

func selectAllTypesByMarker(marker string) SelectStatement {
	return SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes).
		WHERE(sample.AllTypes.Text.EQ(String(marker))).ORDER_BY(sample.AllTypes.VarChar)
}

func TestOptionInsertNoneIsNull(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	const marker = "option-none"
	var inserted optmodel.AllTypes
	err := sample.AllTypes.INSERT(sample.AllTypes.AllColumns).
		MODEL(sqliteAllTypes(marker)).
		RETURNING(sample.AllTypes.AllColumns).
		Query(tx, &inserted)
	require.NoError(t, err)
	require.Equal(t, 20, testutils.AssertOptionFields(t, inserted, false))

	var ptr model.AllTypes
	require.NoError(t, selectAllTypesByMarker(marker).Query(tx, &ptr))
	testutils.AssertPointerFieldsNil(t, ptr)
}

func TestOptionInsertPresentRoundTrip(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	const marker = "option-present"
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	row := sqliteAllTypes(marker)
	// Present zero values must not become NULL.
	row.BooleanPtr = functional.Some(false)
	row.TinyIntPtr = functional.Some[int8](0)
	row.SmallIntPtr = functional.Some[int16](0)
	row.MediumIntPtr = functional.Some[int32](0)
	row.IntegerPtr = functional.Some[int64](0)
	row.BigIntPtr = functional.Some[int64](-9)
	row.DecimalPtr = functional.Some(decimal.Zero)
	row.NumericPtr = functional.Some(decimal.RequireFromString("1.25"))
	row.FloatPtr = functional.Some(0.0)
	row.DoublePtr = functional.Some(1.5)
	row.RealPtr = functional.Some(2.5)
	row.TimePtr = functional.Some(time.Date(0, 1, 1, 3, 4, 5, 0, time.UTC))
	row.DatePtr = functional.Some(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	row.DateTimePtr = functional.Some(ts)
	row.TimestampPtr = functional.Some(ts)
	row.CharPtr = functional.Some("")
	row.VarCharPtr = functional.Some("")
	row.TextPtr = functional.Some("")
	row.BlobPtr = functional.Some([]byte("blob"))
	row.JSONPtr = functional.Some(`{"a":1}`)

	_, err := sample.AllTypes.INSERT(sample.AllTypes.AllColumns).MODEL(row).Exec(tx)
	require.NoError(t, err)

	var opt optmodel.AllTypes
	require.NoError(t, selectAllTypesByMarker(marker).Query(tx, &opt))
	var ptr model.AllTypes
	require.NoError(t, selectAllTypesByMarker(marker).Query(tx, &ptr))

	testutils.AssertOptionFields(t, opt, true)
	testutils.AssertOptionParity(t, opt, ptr)
	testutils.AssertDeepEqual(t, opt, row, testutils.OptionComparable)
}

func TestOptionModelsMixed(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	const marker = "option-models"
	rows := []optmodel.AllTypes{sqliteAllTypes(marker), sqliteAllTypes(marker), sqliteAllTypes(marker)}
	rows[0].VarChar, rows[1].VarChar, rows[2].VarChar = "1", "2", "3"
	rows[0].IntegerPtr = functional.Some[int64](10)
	rows[1].TextPtr = functional.Some("two")
	rows[2].IntegerPtr, rows[2].TextPtr = functional.Some[int64](30), functional.Some("three")

	_, err := sample.AllTypes.INSERT(sample.AllTypes.AllColumns).MODELS(rows).Exec(tx)
	require.NoError(t, err)

	var got []optmodel.AllTypes
	require.NoError(t, selectAllTypesByMarker(marker).Query(tx, &got))
	require.Len(t, got, 3)
	for i := range rows {
		require.Equal(t, rows[i].IntegerPtr, got[i].IntegerPtr, "row %d", i)
		require.Equal(t, rows[i].TextPtr, got[i].TextPtr, "row %d", i)
		require.False(t, got[i].BooleanPtr.IsPresent(), "row %d", i)
	}
}

const optionLinkID = 9001

// selectOptionLink selects the link row the tests insert as optionLinkID.
func selectOptionLink() SelectStatement {
	return SELECT(sample.Link.AllColumns).FROM(sample.Link).WHERE(sample.Link.ID.EQ(Int(optionLinkID)))
}

func TestOptionInsertValues(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := sample.Link.INSERT(sample.Link.ID, sample.Link.URL, sample.Link.Name, sample.Link.Description).
		VALUES(optionLinkID, "https://a", functional.Some("a"), functional.None[string]()).
		VALUES(optionLinkID+1, "https://b", "b", functional.Some("described")).
		Exec(tx)
	require.NoError(t, err)

	var got []optmodel.Link
	require.NoError(t, SELECT(sample.Link.AllColumns).FROM(sample.Link).
		WHERE(sample.Link.ID.GT_EQ(Int(optionLinkID))).ORDER_BY(sample.Link.ID).Query(tx, &got))
	require.Equal(t, []optmodel.Link{
		{ID: optionLinkID, URL: "https://a", Name: "a"},
		{ID: optionLinkID + 1, URL: "https://b", Name: "b", Description: functional.Some("described")},
	}, got)
}

func TestOptionUpdateModel(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := sample.Link.INSERT().
		MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n", Description: functional.Some("a")}).
		Exec(tx)
	require.NoError(t, err)

	update := func(desc functional.Option[string]) optmodel.Link {
		_, updateErr := sample.Link.UPDATE(sample.Link.Description).
			MODEL(optmodel.Link{Description: desc}).
			WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
			Exec(tx)
		require.NoError(t, updateErr)
		var got optmodel.Link
		require.NoError(t, selectOptionLink().Query(tx, &got))
		return got
	}

	require.False(t, update(functional.None[string]()).Description.IsPresent(), "Some to None")
	require.Equal(t, functional.Some("b"), update(functional.Some("b")).Description, "None to Some")
}

func TestOptionUpdateSet(t *testing.T) {
	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := sample.Link.INSERT().
		MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n", Description: functional.Some("a")}).
		Exec(tx)
	require.NoError(t, err)

	_, err = sample.Link.UPDATE(sample.Link.Name, sample.Link.Description).
		SET(functional.Some("renamed"), functional.None[string]()).
		WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
		Exec(tx)
	require.NoError(t, err)

	var got optmodel.Link
	require.NoError(t, selectOptionLink().Query(tx, &got))
	require.Equal(t, optmodel.Link{ID: optionLinkID, URL: "u", Name: "renamed"}, got)

	_, err = sample.Link.UPDATE().
		SET(sample.Link.Description.SET(StringOpt(functional.Some("typed")))).
		WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
		Exec(tx)
	require.NoError(t, err)
	require.NoError(t, selectOptionLink().Query(tx, &got))
	require.Equal(t, functional.Some("typed"), got.Description)

	_, err = sample.Link.UPDATE().
		SET(sample.Link.Description.SET(StringOpt(functional.None[string]()))).
		WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
		Exec(tx)
	require.NoError(t, err)
	require.NoError(t, selectOptionLink().Query(tx, &got))
	require.False(t, got.Description.IsPresent())
}

func TestOptionLiterals(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	var dest struct {
		A            functional.Option[string]    `alias:"a"`
		B            functional.Option[string]    `alias:"b"`
		I            functional.Option[int64]     `alias:"i"`
		INone        functional.Option[int64]     `alias:"i_none"`
		Bool         functional.Option[bool]      `alias:"bool"`
		Float        functional.Option[float64]   `alias:"float"`
		Blob         functional.Option[[]byte]    `alias:"blob"`
		Date         functional.Option[time.Time] `alias:"date"`
		DateNone     functional.Option[time.Time] `alias:"date_none"`
		Time         functional.Option[time.Time] `alias:"time"`
		DateTime     functional.Option[time.Time] `alias:"date_time"`
		DateTimeNone functional.Option[time.Time] `alias:"date_time_none"`
	}
	err := SELECT(
		StringOpt(functional.Some("a")).AS("a"),
		StringOpt(functional.None[string]()).AS("b"),
		Int64Opt(functional.Some[int64](7)).AS("i"),
		Int64Opt(functional.None[int64]()).AS("i_none"),
		BoolOpt(functional.Some(true)).AS("bool"),
		FloatOpt(functional.Some(1.5)).AS("float"),
		BlobOpt(functional.Some([]byte("x"))).AS("blob"),
		DateOpt(functional.Some(ts)).AS("date"),
		DateOpt(functional.None[time.Time]()).AS("date_none"),
		TimeOpt(functional.Some(ts)).AS("time"),
		DateTimeOpt(functional.Some(ts)).AS("date_time"),
		DateTimeOpt(functional.None[time.Time]()).AS("date_time_none"),
	).Query(sampleDB, &dest)
	require.NoError(t, err)

	require.Equal(t, functional.Some("a"), dest.A)
	require.False(t, dest.B.IsPresent())
	require.Equal(t, functional.Some[int64](7), dest.I)
	require.False(t, dest.INone.IsPresent())
	require.Equal(t, functional.Some(true), dest.Bool)
	require.Equal(t, functional.Some(1.5), dest.Float)
	require.Equal(t, functional.Some([]byte("x")), dest.Blob)
	require.True(t, dest.Date.Value().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)), dest.Date)
	require.False(t, dest.DateNone.IsPresent())
	require.Equal(t, "03:04:05", dest.Time.Value().Format("15:04:05"))
	require.True(t, dest.DateTime.Value().Equal(ts), dest.DateTime)
	require.False(t, dest.DateTimeNone.IsPresent())
}

func TestOptionDebugSql(t *testing.T) {
	stmt := sample.Link.INSERT().MODEL(optmodel.Link{ID: 1, URL: "u", Name: "n"}).
		MODEL(optmodel.Link{ID: 2, URL: "u", Name: "n", Description: functional.Some("d")})

	testutils.AssertDebugStatementSql(t, stmt, `
INSERT INTO link
VALUES (1, 'u', 'n', NULL),
       (2, 'u', 'n', 'd');
`)
}

// SQLite read quirks: dynamic typing, so the same Option type reads different storage
// classes.
func TestOptionSQLiteQuirks(t *testing.T) {
	var dest struct {
		DecimalInt  functional.Option[decimal.Decimal] `alias:"decimal_int"`
		DecimalReal functional.Option[decimal.Decimal] `alias:"decimal_real"`
		DecimalText functional.Option[decimal.Decimal] `alias:"decimal_text"`
		TimeText    functional.Option[time.Time]       `alias:"time_text"`
		BoolOne     functional.Option[bool]            `alias:"bool_one"`
		BoolZero    functional.Option[bool]            `alias:"bool_zero"`
	}
	err := SELECT(
		Int(12).AS("decimal_int"),
		Float(1.5).AS("decimal_real"),
		String("2.25").AS("decimal_text"),
		String("10:11:12").AS("time_text"),
		Int(1).AS("bool_one"),
		Int(0).AS("bool_zero"),
	).Query(sampleDB, &dest)
	require.NoError(t, err)

	require.True(t, dest.DecimalInt.Value().Equal(decimal.NewFromInt(12)), dest.DecimalInt)
	require.True(t, dest.DecimalReal.Value().Equal(decimal.RequireFromString("1.5")), dest.DecimalReal)
	require.True(t, dest.DecimalText.Value().Equal(decimal.RequireFromString("2.25")), dest.DecimalText)
	require.Equal(t, "10:11:12", dest.TimeText.Value().Format("15:04:05"))
	require.Equal(t, functional.Some(true), dest.BoolOne)
	require.Equal(t, functional.Some(false), dest.BoolZero)

	// time_ptr as stored text, through the model.
	var row optmodel.AllTypes
	require.NoError(t, SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes).
		WHERE(sample.AllTypes.TimePtr.IS_NOT_NULL()).LIMIT(1).Query(sampleDB, &row))
	require.True(t, row.TimePtr.IsPresent())
}

// A non-UTC time written through SET (as a functional.Option argument) and compared with
// DateOpt, TimeOpt and DateTimeOpt over the same time matches: SQLite normalises the stored
// offset to UTC, and the literals are formatted in UTC. 03:04:05+05:00 on Jan 2 is
// 22:04:05 UTC on Jan 1, so DateOpt must give the UTC date, not the wall-clock one.
func TestOptionTimeLiteralsMatchStoredNonUTCTime(t *testing.T) {
	plus5 := time.FixedZone("PLUS5", 5*60*60)
	tm := time.Date(2024, 1, 2, 3, 4, 5, 0, plus5)

	tx := beginSampleDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := sample.AllTypes.UPDATE(sample.AllTypes.DateTimePtr).
		SET(functional.Some(tm)).
		WHERE(Bool(true)).
		Exec(tx)
	require.NoError(t, err)

	var rows []struct {
		DateTime functional.Option[time.Time] `alias:"date_time"`
		Literal  functional.Option[time.Time] `alias:"literal"`
	}
	err = SELECT(
		DATETIME(sample.AllTypes.DateTimePtr).AS("date_time"),
		DateTimeOpt(functional.Some(tm)).AS("literal"),
	).FROM(sample.AllTypes).
		WHERE(AND(
			DATETIME(sample.AllTypes.DateTimePtr).EQ(DateTimeOpt(functional.Some(tm))),
			DATE(sample.AllTypes.DateTimePtr).EQ(DateOpt(functional.Some(tm))),
			TIME(sample.AllTypes.DateTimePtr).EQ(TimeOpt(functional.Some(tm))),
		)).
		Query(tx, &rows)
	require.NoError(t, err)

	var count struct {
		Total int64 `alias:"total"`
	}
	require.NoError(t, SELECT(COUNT(STAR).AS("total")).FROM(sample.AllTypes).Query(tx, &count))
	require.Positive(t, count.Total)
	require.Len(t, rows, int(count.Total), "every updated row must match its own time")
	for _, row := range rows {
		require.True(t, row.DateTime.Value().Equal(tm), "DATETIME(column) = %v, want the instant %v", row.DateTime, tm)
		require.True(
			t,
			row.Literal.Value().Equal(tm),
			"DateTimeOpt(Some(t)) reads back as %v, want the instant %v",
			row.Literal,
			tm,
		)
	}

	var date struct {
		Date string `alias:"date"`
	}
	require.NoError(t, SELECT(CAST(DateOpt(functional.Some(tm))).AS_TEXT().AS("date")).Query(tx, &date))
	require.Equal(t, "2024-01-01", date.Date)
}
