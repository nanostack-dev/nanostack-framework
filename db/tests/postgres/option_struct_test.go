// This file is Nanostack-only (not in go-jet): functional.Option struct destinations
// (nested structs, custom destinations, SELECT_JSON) against PostgreSQL. See db/UPSTREAM.md.

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/testutils"
	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	dvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/dvds/table"
	"github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/test_sample/model"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/test_sample/table"
	optdvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/jetdb/dvds/model"
	optmodel "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/jetdb/test_sample/model"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// filmWithOriginalLanguage is the nested destination of the NestedOptionStruct tests.
// The dvds schema has no film.original_language_id, so the LEFT JOIN joins a language
// to film 1 only: the other films have no original_language row.
type filmWithOriginalLanguage struct {
	optdvds.Film
	OriginalLanguage functional.Option[optdvds.Language] `alias:"original_language.*"`
}

func originalLanguageJoin() (ReadableTable, *dvds.LanguageTable) {
	originalLanguage := dvds.Language.AS("original_language")
	return dvds.Film.LEFT_JOIN(originalLanguage,
		dvds.Film.LanguageID.EQ(originalLanguage.LanguageID).AND(dvds.Film.FilmID.EQ(Int(1)))), originalLanguage
}

func TestOptionNestedStruct(t *testing.T) {
	films, originalLanguage := originalLanguageJoin()

	var dest []filmWithOriginalLanguage
	err := SELECT(dvds.Film.AllColumns, originalLanguage.AllColumns).
		FROM(films).
		WHERE(dvds.Film.FilmID.IN(Int(1), Int(2))).
		ORDER_BY(dvds.Film.FilmID).
		Query(db, &dest)
	require.NoError(t, err)

	require.Len(t, dest, 2)
	lang, ok := dest[0].OriginalLanguage.Get()
	require.True(t, ok)
	require.Equal(t, int32(1), lang.LanguageID)
	require.Equal(t, "English", lang.Name[:7])
	require.False(t, dest[1].OriginalLanguage.IsPresent())
	// An enum column in Option mode.
	require.Equal(t, functional.Some(optdvds.MpaaRating_Pg), dest[0].Rating)

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
		Query(db, &actors)
	require.NoError(t, err)

	require.Len(t, actors, 1)
	require.Greater(t, len(actors[0].Films), 1)
	require.Equal(t, int32(1), actors[0].Films[0].FilmID)
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
		NoPayload  functional.Option[jsonPayload] `json_column:"no_payload"`
	}
	err := SELECT(
		sample.AllTypes.TextPtr,
		MAX(sample.AllTypes.IntegerPtr).AS("max_integer"),
		Raw("json_build_object('a', 1, 'b', 'x')").AS("payload"),
		Raw("NULL::json").AS("no_payload"),
	).
		FROM(sample.AllTypes).
		GROUP_BY(sample.AllTypes.TextPtr).
		ORDER_BY(sample.AllTypes.TextPtr.ASC().NULLS_LAST()).
		Query(db, &dest)
	require.NoError(t, err)
	require.Len(t, dest, 2)
	require.True(t, dest[0].TextPtr.IsPresent())
	require.True(t, dest[0].MaxInteger.IsPresent())
	require.Equal(t, functional.Some(jsonPayload{A: 1, B: "x"}), dest[0].Payload)
	require.False(t, dest[0].NoPayload.IsPresent())
	require.False(t, dest[1].TextPtr.IsPresent())
	require.False(t, dest[1].MaxInteger.IsPresent())

	var none struct {
		MaxInteger functional.Option[int64] `alias:"max_integer"`
	}
	err = SELECT(MAX(sample.AllTypes.IntegerPtr).AS("max_integer")).
		FROM(sample.AllTypes).
		WHERE(sample.AllTypes.Serial.LT(Int(0))).
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
			sample.AllTypes.JSON, sample.AllTypes.JSONPtr,
			sample.AllTypes.Jsonb, sample.AllTypes.JsonbPtr,
			sample.AllTypes.JsonbArray,
			sample.AllTypes.TextMultiDimArray, sample.AllTypes.TextMultiDimArrayPtr,
		),
		CAST(sample.AllTypes.JSONPtr).AS_TEXT().AS("jsonPtr"),
		CAST(sample.AllTypes.JSON).AS_TEXT().AS("JSON"),
		CAST(sample.AllTypes.JsonbPtr).AS_TEXT().AS("jsonbPtr"),
		CAST(sample.AllTypes.Jsonb).AS_TEXT().AS("Jsonb"),
		CAST(sample.AllTypes.JsonbArray).AS_TEXT_ARRAY().AS("JsonbArray"),
		CAST(sample.AllTypes.TextMultiDimArray).AS_TEXT().AS("TextMultiDimArray"),
		CAST(sample.AllTypes.TextMultiDimArrayPtr).AS_TEXT().AS("TextMultiDimArrayPtr"),
	).FROM(sample.AllTypes)

	var opt []optmodel.AllTypes
	require.NoError(t, stmt.QueryContext(ctx, db, &opt))
	var ptr []model.AllTypes
	require.NoError(t, stmt.QueryContext(ctx, db, &ptr))

	require.Len(t, opt, len(ptr))
	require.GreaterOrEqual(t, len(opt), 2)
	present, absent := 0, 0
	for i := range opt {
		testutils.AssertOptionParity(t, opt[i], ptr[i])
		if opt[i].TimestampzPtr.IsPresent() {
			present++
		} else {
			absent++
		}
	}
	require.Positive(t, present, "SELECT_JSON reads present Options, including Option[time.Time]")
	require.Positive(t, absent, "SELECT_JSON reads null as None")
}
