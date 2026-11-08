// This file is Nanostack-only (not in go-jet): functional.Option models against MySQL
// and MariaDB. See db/UPSTREAM.md.

package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	dvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/mysql/dvds/table"
	"github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/mysql/test_sample/model"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/mysql/test_sample/table"
	optdvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/mysql/dvds/model"
	optmodel "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/mysql/test_sample/model"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func selectAllTypes() SelectStatement {
	return SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes).ORDER_BY(sample.AllTypes.ID)
}

// readAllTypes reads the all_types rows in Option mode and in pointer mode.
func readAllTypes(t *testing.T, db qrm.Queryable, stmt Statement) ([]optmodel.AllTypes, []model.AllTypes) {
	t.Helper()
	var opt []optmodel.AllTypes
	require.NoError(t, stmt.Query(db, &opt))
	var ptr []model.AllTypes
	require.NoError(t, stmt.Query(db, &ptr))
	require.Len(t, opt, len(ptr))
	for i := range opt {
		testutils.AssertOptionParity(t, opt[i], ptr[i])
	}
	return opt, ptr
}

func TestOptionScanParity(t *testing.T) {
	opt, _ := readAllTypes(t, db, selectAllTypes())
	require.GreaterOrEqual(t, len(opt), 2)
	// Row 0 has every nullable column set, row 1 none (test data).
	require.Positive(t, testutils.AssertOptionFields(t, opt[0], true))
	testutils.AssertOptionFields(t, opt[1], false)
}

func TestOptionInsertModelRoundTrip(t *testing.T) {
	opt, _ := readAllTypes(t, db, selectAllTypes())

	present := opt[0]
	present.ID = 1001
	// Present zero values must not become NULL.
	present.SmallIntPtr = functional.Some[int16](0)
	present.IntegerPtr = functional.Some[int32](0)
	present.BooleanPtr = functional.Some(false)
	present.TextPtr = functional.Some("")
	present.VarCharPtr = functional.Some("")

	absent := opt[1]
	absent.ID = 1002

	// TIME columns read as 0000-01-01 hh:mm:ss, a year the driver refuses to write.
	clock := func(t time.Time) time.Time {
		return time.Date(1, 1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	}
	present.Time, absent.Time = clock(present.Time), clock(absent.Time)
	present.TimePtr = functional.Some(clock(present.TimePtr.Value()))

	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// timestamp_ptr is left out, as in upstream's TestAllTypesInsertModel: MySQL fills
	// a TIMESTAMP column it is not given.
	columns := sample.AllTypes.AllColumns.Except(sample.AllTypes.TimestampPtr)
	insert := sample.AllTypes.INSERT(columns).MODEL(present).MODEL(absent)

	var inserted []optmodel.AllTypes
	if sourceIsMariaDB() {
		require.NoError(t, insert.RETURNING(sample.AllTypes.AllColumns).Query(tx, &inserted))
	} else {
		_, err = insert.Exec(tx)
		require.NoError(t, err)
		require.NoError(t, selectAllTypes().WHERE(sample.AllTypes.ID.GT_EQ(Int(1001))).Query(tx, &inserted))
	}
	require.Len(t, inserted, 2)

	require.Equal(t, functional.Some[int32](0), inserted[0].IntegerPtr)
	require.Equal(t, functional.Some(false), inserted[0].BooleanPtr)
	require.Equal(t, functional.Some(""), inserted[0].TextPtr)
	inserted[1].TimestampPtr = functional.None[time.Time]() // filled by the server, see above
	testutils.AssertOptionFields(t, inserted[1], false)

	_, ptr := readAllTypes(t, tx, selectAllTypes().WHERE(sample.AllTypes.ID.GT_EQ(Int(1001))))
	require.Len(t, ptr, 2)
	ptr[1].TimestampPtr = nil
	testutils.AssertPointerFieldsNil(t, ptr[1])

	inserted[0].TimestampPtr = present.TimestampPtr
	for i, want := range []optmodel.AllTypes{present, absent} {
		require.Equal(t, want.Time.Format("15:04:05"), inserted[i].Time.Format("15:04:05"))
		inserted[i].Time = want.Time
	}
	require.Equal(t, present.TimePtr.Value().Format("15:04:05"), inserted[0].TimePtr.Value().Format("15:04:05"))
	inserted[0].TimePtr = present.TimePtr
	testutils.AssertDeepEqual(t, inserted[0], present, testutils.OptionComparable)
	testutils.AssertDeepEqual(t, inserted[1], absent, testutils.OptionComparable)
}

const optionLinkID = 9001

// selectOptionLink selects the link row the tests insert as optionLinkID.
func selectOptionLink() SelectStatement {
	return SELECT(sample.Link.AllColumns).FROM(sample.Link).WHERE(sample.Link.ID.EQ(Int(optionLinkID)))
}

func TestOptionModelsAndValues(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = sample.Link.INSERT().MODELS([]optmodel.Link{
		{ID: optionLinkID, URL: "https://a", Name: "a"},
		{ID: optionLinkID + 1, URL: "https://b", Name: "b", Description: functional.Some("")},
		{ID: optionLinkID + 2, URL: "https://c", Name: "c", Description: functional.Some("c")},
	}).Exec(tx)
	require.NoError(t, err)

	_, err = sample.Link.INSERT(sample.Link.ID, sample.Link.URL, sample.Link.Name, sample.Link.Description).
		VALUES(optionLinkID+3, "https://d", functional.Some("d"), functional.None[string]()).
		Exec(tx)
	require.NoError(t, err)

	var got []optmodel.Link
	require.NoError(t, SELECT(sample.Link.AllColumns).FROM(sample.Link).
		WHERE(sample.Link.ID.GT_EQ(Int(optionLinkID))).ORDER_BY(sample.Link.ID).Query(tx, &got))
	require.Equal(t, []optmodel.Link{
		{ID: optionLinkID, URL: "https://a", Name: "a"},
		{ID: optionLinkID + 1, URL: "https://b", Name: "b", Description: functional.Some("")},
		{ID: optionLinkID + 2, URL: "https://c", Name: "c", Description: functional.Some("c")},
		{ID: optionLinkID + 3, URL: "https://d", Name: "d"},
	}, got)
}

func TestOptionUpdateModelAndSet(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = sample.Link.INSERT().
		MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n", Description: functional.Some("a")}).
		Exec(tx)
	require.NoError(t, err)

	read := func() optmodel.Link {
		var got optmodel.Link
		require.NoError(t, selectOptionLink().Query(tx, &got))
		return got
	}
	updateModel := func(desc functional.Option[string]) {
		_, updateErr := sample.Link.UPDATE(sample.Link.Description).
			MODEL(optmodel.Link{Description: desc}).
			WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
			Exec(tx)
		require.NoError(t, updateErr)
	}

	updateModel(functional.None[string]())
	require.False(t, read().Description.IsPresent(), "Some to None")
	updateModel(functional.Some("b"))
	require.Equal(t, functional.Some("b"), read().Description, "None to Some")

	_, err = sample.Link.UPDATE(sample.Link.Name, sample.Link.Description).
		SET(functional.Some("renamed"), functional.None[string]()).
		WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
		Exec(tx)
	require.NoError(t, err)
	require.Equal(t, optmodel.Link{ID: optionLinkID, URL: "u", Name: "renamed"}, read())

	for _, desc := range []functional.Option[string]{functional.Some("typed"), functional.None[string]()} {
		_, err = sample.Link.UPDATE().
			SET(sample.Link.Description.SET(StringOpt(desc))).
			WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
			Exec(tx)
		require.NoError(t, err)
		require.Equal(t, desc, read().Description)
	}

	// ON DUPLICATE KEY UPDATE with an Opt literal.
	_, err = sample.Link.INSERT().
		MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n"}).
		ON_DUPLICATE_KEY_UPDATE(sample.Link.Description.SET(StringOpt(functional.Some("dup")))).
		Exec(tx)
	require.NoError(t, err)
	require.Equal(t, functional.Some("dup"), read().Description)
}

func TestOptionLiterals(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")

	var dest struct {
		Str       functional.Option[string]          `alias:"str"`
		StrNone   functional.Option[string]          `alias:"str_none"`
		I32       functional.Option[int32]           `alias:"i32"`
		I64None   functional.Option[int64]           `alias:"i64_none"`
		U64       functional.Option[uint64]          `alias:"u64"`
		Bool      functional.Option[bool]            `alias:"bool"`
		Float     functional.Option[float64]         `alias:"float"`
		Decimal   functional.Option[decimal.Decimal] `alias:"decimal"`
		UUID      functional.Option[uuid.UUID]       `alias:"uuid"`
		Blob      functional.Option[[]byte]          `alias:"blob"`
		Date      functional.Option[time.Time]       `alias:"date"`
		Time      functional.Option[time.Time]       `alias:"time"`
		DateTime  functional.Option[time.Time]       `alias:"date_time"`
		Timestamp functional.Option[time.Time]       `alias:"timestamp"`
		TSNone    functional.Option[time.Time]       `alias:"ts_none"`
	}
	err := SELECT(
		StringOpt(functional.Some("a")).AS("str"),
		StringOpt(functional.None[string]()).AS("str_none"),
		Int32Opt(functional.Some[int32](32)).AS("i32"),
		Int64Opt(functional.None[int64]()).AS("i64_none"),
		Uint64Opt(functional.Some[uint64](18446744073709551615)).AS("u64"),
		BoolOpt(functional.Some(true)).AS("bool"),
		FloatOpt(functional.Some(1.5)).AS("float"),
		DecimalOpt(functional.Some("3.25")).AS("decimal"),
		UUIDOpt(functional.Some(id)).AS("uuid"),
		BlobOpt(functional.Some([]byte("bytes"))).AS("blob"),
		DateOpt(functional.Some(ts)).AS("date"),
		TimeOpt(functional.Some(ts)).AS("time"),
		DateTimeOpt(functional.Some(ts)).AS("date_time"),
		TimestampOpt(functional.Some(ts)).AS("timestamp"),
		DateTimeOpt(functional.None[time.Time]()).AS("ts_none"),
	).Query(db, &dest)
	require.NoError(t, err)

	require.Equal(t, functional.Some("a"), dest.Str)
	require.False(t, dest.StrNone.IsPresent())
	require.Equal(t, functional.Some[int32](32), dest.I32)
	require.False(t, dest.I64None.IsPresent())
	require.Equal(t, functional.Some[uint64](18446744073709551615), dest.U64)
	require.Equal(t, functional.Some(true), dest.Bool)
	require.Equal(t, functional.Some(1.5), dest.Float)
	require.True(t, dest.Decimal.Value().Equal(decimal.RequireFromString("3.25")), dest.Decimal)
	require.Equal(t, functional.Some(id), dest.UUID)
	require.Equal(t, functional.Some([]byte("bytes")), dest.Blob)
	require.Equal(t, "2024-01-02", dest.Date.Value().Format("2006-01-02"))
	// TimeOpt binds a time.Time, which MySQL 8.4 casts correctly (unlike the string
	// parameter of upstream's TestCast, a known failure there).
	require.Equal(t, "03:04:05", dest.Time.Value().Format("15:04:05"))
	require.True(t, dest.DateTime.Value().Equal(ts), dest.DateTime)
	require.True(t, dest.Timestamp.Value().Equal(ts), dest.Timestamp)
	require.False(t, dest.TSNone.IsPresent())
}

// filmWithOriginalLanguage is the nested destination of the NestedOptionStruct tests.
type filmWithOriginalLanguage struct {
	optdvds.Film
	OriginalLanguage functional.Option[optdvds.Language] `alias:"original_language.*"`
}

func TestOptionNestedStruct(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = dvds.Film.UPDATE(dvds.Film.OriginalLanguageID).SET(Int(2)).
		WHERE(dvds.Film.FilmID.EQ(Int(1))).Exec(tx)
	require.NoError(t, err)

	originalLanguage := dvds.Language.AS("original_language")
	films := dvds.Film.LEFT_JOIN(originalLanguage, dvds.Film.OriginalLanguageID.EQ(originalLanguage.LanguageID))

	var dest []filmWithOriginalLanguage
	err = SELECT(dvds.Film.AllColumns, originalLanguage.AllColumns).
		FROM(films).
		WHERE(dvds.Film.FilmID.IN(Int(1), Int(2))).
		ORDER_BY(dvds.Film.FilmID).
		Query(tx, &dest)
	require.NoError(t, err)

	require.Len(t, dest, 2)
	lang, ok := dest[0].OriginalLanguage.Get()
	require.True(t, ok)
	require.Equal(t, uint8(2), lang.LanguageID)
	require.Equal(t, "Italian", lang.Name)
	require.Equal(t, functional.Some[uint8](2), dest[0].OriginalLanguageID)
	require.False(t, dest[1].OriginalLanguage.IsPresent())
	require.Equal(t, functional.Some(optdvds.FilmRating_Pg), dest[0].Rating, "an enum column in Option mode")

	var actors []struct {
		optdvds.Actor
		Films []filmWithOriginalLanguage
	}
	err = SELECT(dvds.Actor.AllColumns, dvds.Film.AllColumns, originalLanguage.AllColumns).
		FROM(dvds.Actor.
			INNER_JOIN(dvds.FilmActor, dvds.FilmActor.ActorID.EQ(dvds.Actor.ActorID)).
			INNER_JOIN(films, dvds.Film.FilmID.EQ(dvds.FilmActor.FilmID))).
		WHERE(dvds.Actor.ActorID.EQ(Int(1))).
		ORDER_BY(dvds.Film.FilmID).
		Query(tx, &actors)
	require.NoError(t, err)

	require.Len(t, actors, 1)
	require.Greater(t, len(actors[0].Films), 1)
	require.Equal(t, uint16(1), actors[0].Films[0].FilmID)
	require.True(t, actors[0].Films[0].OriginalLanguage.IsPresent())
	for _, f := range actors[0].Films[1:] {
		require.False(t, f.OriginalLanguage.IsPresent(), "film %d", f.FilmID)
	}
}

type jsonPayload struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func TestOptionCustomDestination(t *testing.T) {
	var dest []struct {
		TextPtr    functional.Option[string]      `alias:"all_types.text_ptr"`
		MaxInteger functional.Option[int64]       `alias:"max_integer"`
		Payload    functional.Option[jsonPayload] `json_column:"payload"`
	}
	err := SELECT(
		sample.AllTypes.TextPtr,
		MAX(sample.AllTypes.IntegerPtr).AS("max_integer"),
		Raw("JSON_OBJECT('a', 1, 'b', 'x')").AS("payload"),
	).
		FROM(sample.AllTypes).
		GROUP_BY(sample.AllTypes.TextPtr).
		ORDER_BY(sample.AllTypes.TextPtr.DESC()).
		Query(db, &dest)
	require.NoError(t, err)
	require.Len(t, dest, 2)
	require.True(t, dest[0].TextPtr.IsPresent())
	require.True(t, dest[0].MaxInteger.IsPresent())
	require.Equal(t, functional.Some(jsonPayload{A: 1, B: "x"}), dest[0].Payload)
	require.False(t, dest[1].TextPtr.IsPresent())
	require.False(t, dest[1].MaxInteger.IsPresent())

	var none struct {
		MaxInteger functional.Option[int64] `alias:"max_integer"`
	}
	err = SELECT(MAX(sample.AllTypes.IntegerPtr).AS("max_integer")).
		FROM(sample.AllTypes).
		WHERE(sample.AllTypes.ID.LT(Int(0))).
		Query(db, &none)
	require.NoError(t, err)
	require.False(t, none.MaxInteger.IsPresent(), "MAX over nothing is NULL")

	// The Rows.Scan path.
	rows, err := selectAllTypes().Rows(context.Background(), db)
	require.NoError(t, err)
	defer rows.Close()
	var scanned []optmodel.AllTypes
	for rows.Next() {
		var row optmodel.AllTypes
		require.NoError(t, rows.Scan(&row))
		scanned = append(scanned, row)
	}
	require.NoError(t, rows.Err())
	require.GreaterOrEqual(t, len(scanned), 2)
	require.True(t, scanned[0].TextPtr.IsPresent())
	require.False(t, scanned[1].TextPtr.IsPresent())
}

func TestOptionSelectJson(t *testing.T) {
	stmt := SELECT_JSON_ARR(
		sample.AllTypes.AllColumns.Except(
			sample.AllTypes.JSON,
			sample.AllTypes.JSONPtr,
			sample.AllTypes.Bit,
			sample.AllTypes.BitPtr,
		),
		CAST(sample.AllTypes.JSON).AS_CHAR().AS("Json"),
		CAST(sample.AllTypes.JSONPtr).AS_CHAR().AS("JsonPtr"),
		CAST(sample.AllTypes.Bit).AS_CHAR().AS("Bit"),
		CAST(sample.AllTypes.BitPtr).AS_CHAR().AS("BitPtr"),
	).FROM(sample.AllTypes)

	var opt []optmodel.AllTypes
	require.NoError(t, stmt.Query(db, &opt))
	var ptr []model.AllTypes
	require.NoError(t, stmt.Query(db, &ptr))

	require.Len(t, opt, len(ptr))
	require.GreaterOrEqual(t, len(opt), 2)
	present, absent := 0, 0
	for i := range opt {
		testutils.AssertOptionParity(t, opt[i], ptr[i])
		if opt[i].DateTimePtr.IsPresent() {
			present++
		} else {
			absent++
		}
	}
	require.Positive(t, present, "SELECT_JSON reads present Options, including Option[time.Time]")
	require.Positive(t, absent, "SELECT_JSON reads null as None")
}

func TestOptionDebugSql(t *testing.T) {
	stmt := sample.Link.INSERT().
		MODEL(optmodel.Link{ID: 1, URL: "u", Name: "n"}).
		MODEL(optmodel.Link{ID: 2, URL: "u", Name: "n", Description: functional.Some("d")})

	testutils.AssertDebugStatementSql(t, stmt, `
INSERT INTO test_sample.link
VALUES (1, 'u', 'n', NULL),
       (2, 'u', 'n', 'd');
`)
}

// MySQL read quirks: the default DSN has no parseTime, so time types arrive as []byte;
// with parseTime=true they arrive as time.Time. Both must read the same.
func TestOptionMySQLParseTime(t *testing.T) {
	parseTimeDB, err := sql.Open("mysql", dbconfig.MySQLConnectionString(sourceIsMariaDB(), "")+"?parseTime=true")
	require.NoError(t, err)
	defer parseTimeDB.Close()

	var raw, parsed []optmodel.AllTypes
	require.NoError(t, selectAllTypes().Query(db, &raw))
	require.NoError(t, selectAllTypes().Query(parseTimeDB, &parsed))
	require.Len(t, parsed, len(raw))

	for i := range raw {
		for _, pair := range [][2]functional.Option[time.Time]{
			{raw[i].DatePtr, parsed[i].DatePtr},
			{raw[i].TimePtr, parsed[i].TimePtr},
			{raw[i].DateTimePtr, parsed[i].DateTimePtr},
			{raw[i].TimestampPtr, parsed[i].TimestampPtr},
		} {
			require.Equal(t, pair[0].IsPresent(), pair[1].IsPresent(), "row %d", i)
			require.True(t, pair[0].Value().Equal(pair[1].Value()), "row %d: %v vs %v", i, pair[0], pair[1])
		}
		require.True(t, raw[i].DateTime.Equal(parsed[i].DateTime), "row %d", i)
	}
}

func TestOptionMySQLUnsignedAndBool(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	_, err = sample.AllTypes.UPDATE().
		SET(sample.AllTypes.UBigIntPtr.SET(Uint64Opt(functional.Some[uint64](18446744073709551615))),
			sample.AllTypes.BooleanPtr.SET(BoolOpt(functional.Some(true))),
			sample.AllTypes.BitPtr.SET(String("1"))).
		WHERE(sample.AllTypes.ID.EQ(Int(1))).
		Exec(tx)
	require.NoError(t, err)

	opt, _ := readAllTypes(t, tx, selectAllTypes().WHERE(sample.AllTypes.ID.EQ(Int(1))))
	require.Len(t, opt, 1)
	require.Equal(t, functional.Some[uint64](18446744073709551615), opt[0].UBigIntPtr)
	require.Equal(t, functional.Some(true), opt[0].BooleanPtr, "TINYINT(1)")
	require.True(t, opt[0].BitPtr.IsPresent())
}
