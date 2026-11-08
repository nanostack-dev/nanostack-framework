// This file is Nanostack-only (not in go-jet): functional.Option models against
// PostgreSQL and CockroachDB, under the pq and pgx drivers. See db/UPSTREAM.md.

package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/test_sample/model"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/test_sample/table"
	optmodel "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/jetdb/test_sample/model"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func selectAllTypes() SelectStatement {
	return SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes).ORDER_BY(sample.AllTypes.Serial)
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
	present.Serial = 1001
	// Present zero values must not become NULL.
	present.SmallIntPtr = functional.Some[int16](0)
	present.IntegerPtr = functional.Some[int32](0)
	present.BooleanPtr = functional.Some(false)
	present.TextPtr = functional.Some("")
	present.VarCharPtr = functional.Some("")

	absent := opt[1]
	absent.Serial = 1002

	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
		var inserted []optmodel.AllTypes
		err := sample.AllTypes.INSERT(sample.AllTypes.AllColumns).
			MODEL(present).
			MODEL(absent).
			RETURNING(sample.AllTypes.AllColumns).
			Query(tx, &inserted)
		require.NoError(t, err)
		require.Len(t, inserted, 2)

		testutils.AssertOptionFields(t, inserted[0], true)
		require.Equal(t, functional.Some[int32](0), inserted[0].IntegerPtr)
		require.Equal(t, functional.Some(false), inserted[0].BooleanPtr)
		require.Equal(t, functional.Some(""), inserted[0].TextPtr)
		testutils.AssertOptionFields(t, inserted[1], false)

		// The pointer-model read of the same rows agrees: None was written as NULL.
		_, ptr := readAllTypes(t, tx, selectAllTypes().WHERE(sample.AllTypes.Serial.GT_EQ(Int(1001))))
		require.Len(t, ptr, 2)
		testutils.AssertPointerFieldsNil(t, ptr[1])

		testutils.AssertDeepEqual(t, inserted[0], present, testutils.OptionComparable)
		testutils.AssertDeepEqual(t, inserted[1], absent, testutils.OptionComparable)
	})
}

const optionLinkID = 9001

// selectOptionLink selects the link row the tests insert as optionLinkID.
func selectOptionLink() SelectStatement {
	return SELECT(sample.Link.AllColumns).FROM(sample.Link).WHERE(sample.Link.ID.EQ(Int(optionLinkID)))
}

func TestOptionModelsAndValues(t *testing.T) {
	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
		_, err := sample.Link.INSERT().MODELS([]optmodel.Link{
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
	})
}

func TestOptionUpdateModel(t *testing.T) {
	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
		_, err := sample.Link.INSERT().
			MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n", Description: functional.Some("a")}).
			Exec(tx)
		require.NoError(t, err)

		update := func(desc functional.Option[string]) optmodel.Link {
			var got optmodel.Link
			updateErr := sample.Link.UPDATE(sample.Link.Description).
				MODEL(optmodel.Link{Description: desc}).
				WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
				RETURNING(sample.Link.AllColumns).
				Query(tx, &got)
			require.NoError(t, updateErr)
			return got
		}

		require.False(t, update(functional.None[string]()).Description.IsPresent(), "Some to None")
		require.Equal(t, functional.Some("b"), update(functional.Some("b")).Description, "None to Some")
	})
}

func TestOptionUpdateSet(t *testing.T) {
	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
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

		for _, desc := range []functional.Option[string]{functional.Some("typed"), functional.None[string]()} {
			_, err = sample.Link.UPDATE().
				SET(sample.Link.Description.SET(TextOpt(desc))).
				WHERE(sample.Link.ID.EQ(Int(optionLinkID))).
				Exec(tx)
			require.NoError(t, err)
			require.NoError(t, selectOptionLink().Query(tx, &got))
			require.Equal(t, desc, got.Description)
		}

		// A typed NULL in an integer column.
		_, err = sample.AllTypes.UPDATE().
			SET(sample.AllTypes.IntegerPtr.SET(Int32Opt(functional.None[int32]())),
				sample.AllTypes.SmallIntPtr.SET(Int16Opt(functional.Some[int16](7)))).
			WHERE(sample.AllTypes.Serial.EQ(Int(1))).
			Exec(tx)
		require.NoError(t, err)
		var row optmodel.AllTypes
		require.NoError(t, selectAllTypes().WHERE(sample.AllTypes.Serial.EQ(Int(1))).Query(tx, &row))
		require.False(t, row.IntegerPtr.IsPresent())
		require.Equal(t, functional.Some[int16](7), row.SmallIntPtr)
	})
}

func TestOptionOnConflict(t *testing.T) {
	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
		insert := func(desc functional.Option[string]) optmodel.Link {
			var got optmodel.Link
			err := sample.Link.INSERT().
				MODEL(optmodel.Link{ID: optionLinkID, URL: "u", Name: "n", Description: desc}).
				ON_CONFLICT(sample.Link.ID).
				DO_UPDATE(SET(sample.Link.Description.SET(TextOpt(desc)))).
				RETURNING(sample.Link.AllColumns).
				Query(tx, &got)
			require.NoError(t, err)
			return got
		}
		require.Equal(t, functional.Some("first"), insert(functional.Some("first")).Description)
		require.False(t, insert(functional.None[string]()).Description.IsPresent())
	})
}

func TestOptionLiterals(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	tsNewYork := time.Date(2024, 7, 1, 9, 30, 0, 0, newYork)
	id := uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")

	var dest struct {
		Str        functional.Option[string]          `alias:"str"`
		StrNone    functional.Option[string]          `alias:"str_none"`
		VarChar    functional.Option[string]          `alias:"var_char"`
		Char       functional.Option[string]          `alias:"char"`
		I16        functional.Option[int16]           `alias:"i16"`
		I32None    functional.Option[int32]           `alias:"i32_none"`
		I64        functional.Option[int64]           `alias:"i64"`
		Bool       functional.Option[bool]            `alias:"bool"`
		Real       functional.Option[float32]         `alias:"real"`
		Double     functional.Option[float64]         `alias:"double"`
		Decimal    functional.Option[decimal.Decimal] `alias:"decimal"`
		UUID       functional.Option[uuid.UUID]       `alias:"uuid"`
		UUIDNone   functional.Option[uuid.UUID]       `alias:"uuid_none"`
		Bytea      functional.Option[[]byte]          `alias:"bytea"`
		Date       functional.Option[time.Time]       `alias:"date"`
		Time       functional.Option[time.Time]       `alias:"time"`
		Timestamp  functional.Option[time.Time]       `alias:"timestamp"`
		Timestampz functional.Option[time.Time]       `alias:"timestampz"`
		TSNone     functional.Option[time.Time]       `alias:"ts_none"`
	}
	err = SELECT(
		StringOpt(functional.Some("a")).AS("str"),
		StringOpt(functional.None[string]()).AS("str_none"),
		VarCharOpt(10)(functional.Some("v")).AS("var_char"),
		CharOpt(3)(functional.Some("c")).AS("char"),
		Int16Opt(functional.Some[int16](16)).AS("i16"),
		Int32Opt(functional.None[int32]()).AS("i32_none"),
		Int64Opt(functional.Some[int64](64)).AS("i64"),
		BoolOpt(functional.Some(true)).AS("bool"),
		RealOpt(functional.Some[float32](1.5)).AS("real"),
		DoubleOpt(functional.Some(2.5)).AS("double"),
		DecimalOpt(functional.Some("3.25")).AS("decimal"),
		UUIDOpt(functional.Some(id)).AS("uuid"),
		UUIDOpt(functional.None[uuid.UUID]()).AS("uuid_none"),
		ByteaOpt(functional.Some([]byte("bytes"))).AS("bytea"),
		DateOpt(functional.Some(ts)).AS("date"),
		TimeOpt(functional.Some(ts)).AS("time"),
		TimestampOpt(functional.Some(ts)).AS("timestamp"),
		TimestampzOpt(functional.Some(tsNewYork)).AS("timestampz"),
		TimestampzOpt(functional.None[time.Time]()).AS("ts_none"),
	).Query(db, &dest)
	require.NoError(t, err)

	require.Equal(t, functional.Some("a"), dest.Str)
	require.False(t, dest.StrNone.IsPresent())
	require.Equal(t, functional.Some("v"), dest.VarChar)
	require.Equal(t, "c", dest.Char.Value()[:1])
	require.Equal(t, functional.Some[int16](16), dest.I16)
	require.False(t, dest.I32None.IsPresent())
	require.Equal(t, functional.Some[int64](64), dest.I64)
	require.Equal(t, functional.Some(true), dest.Bool)
	require.Equal(t, functional.Some[float32](1.5), dest.Real)
	require.Equal(t, functional.Some(2.5), dest.Double)
	require.True(t, dest.Decimal.Value().Equal(decimal.RequireFromString("3.25")), dest.Decimal)
	require.Equal(t, functional.Some(id), dest.UUID)
	require.False(t, dest.UUIDNone.IsPresent())
	require.Equal(t, functional.Some([]byte("bytes")), dest.Bytea)
	require.Equal(t, "2024-01-02", dest.Date.Value().Format("2006-01-02"))
	require.Equal(t, "03:04:05", dest.Time.Value().Format("15:04:05"))
	require.True(t, dest.Timestamp.Value().Equal(ts), dest.Timestamp)
	require.True(
		t,
		dest.Timestampz.Value().Equal(tsNewYork),
		"a timestamptz written in New York reads back as the same instant: %v",
		dest.Timestampz,
	)
	require.False(t, dest.TSNone.IsPresent())
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

func TestOptionPostgresArrays(t *testing.T) {
	type arrays struct {
		ID             int32                             `sql:"primary_key" alias:"sample_arrays.id"`
		Int2ArrayPtr   functional.Option[pq.StringArray] `                  alias:"sample_arrays.int2_array_ptr"`
		TimestampArray functional.Option[pq.StringArray] `                  alias:"sample_arrays.timestamp_array"`
		BoolArray      functional.Option[pq.BoolArray]   `                  alias:"sample_arrays.bool_array"`
	}
	stmt := SELECT(
		sample.SampleArrays.ID,
		sample.SampleArrays.Int2ArrayPtr,
		sample.SampleArrays.TimestampArray,
		sample.SampleArrays.BoolArray,
	).
		FROM(sample.SampleArrays).
		ORDER_BY(sample.SampleArrays.ID)

	testutils.ExecuteInTxAndRollback(t, db, func(tx qrm.DB) {
		var before []arrays
		require.NoError(t, stmt.Query(tx, &before))
		require.NotEmpty(t, before)
		first := before[0].ID

		_, err := sample.SampleArrays.UPDATE(sample.SampleArrays.Int2ArrayPtr, sample.SampleArrays.TimestampArray).
			SET(functional.Some(pq.StringArray{}), functional.None[pq.StringArray]()).
			WHERE(sample.SampleArrays.ID.EQ(Int32(first))).
			Exec(tx)
		require.NoError(t, err)

		var after []arrays
		require.NoError(t, stmt.WHERE(sample.SampleArrays.ID.EQ(Int32(first))).Query(tx, &after))
		require.Len(t, after, 1)
		got, ok := after[0].Int2ArrayPtr.Get()
		require.True(t, ok, "Some(empty array) is not None")
		require.Empty(t, got)
		require.False(t, after[0].TimestampArray.IsPresent())
		require.Equal(t, before[0].BoolArray, after[0].BoolArray)
	})
}

// The literal file header recommends Int64Opt and DoubleOpt where the server must infer
// the type from the argument alone: their None is a typed NULL, so a bare projection works
// on PostgreSQL and on CockroachDB, under both drivers. IntOpt and FloatOpt carry no cast,
// like Int and Float (see TestOptionUncastLiterals).
func TestOptionTypedNoneProjection(t *testing.T) {
	var dest struct {
		I64    functional.Option[int64]   `alias:"i64"`
		Double functional.Option[float64] `alias:"double"`
	}
	err := SELECT(
		Int64Opt(functional.None[int64]()).AS("i64"),
		DoubleOpt(functional.None[float64]()).AS("double"),
	).Query(db, &dest)
	require.NoError(t, err)
	require.False(t, dest.I64.IsPresent())
	require.False(t, dest.Double.IsPresent())
}

// IntOpt and FloatOpt serialize like Int and Float: a bare placeholder with no cast, so
// None is an untyped NULL, as the literal file header says.
func TestOptionUncastLiterals(t *testing.T) {
	testutils.AssertStatementSql(t, SELECT(IntOpt(functional.None[int64]()), FloatOpt(functional.None[float64]())), `
SELECT $1,
     $2;
`, nil, nil)
	testutils.AssertStatementSql(t, SELECT(Int64Opt(functional.None[int64]()), DoubleOpt(functional.None[float64]())), `
SELECT $1::bigint,
     $2::double precision;
`, nil, nil)
}
