// This file is Nanostack-only (not in go-jet): functional.Option struct destinations
// (nested structs, custom destinations, JSON columns) against SQLite. See db/UPSTREAM.md.

package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/sqlite"
	optsakila "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/sqlite/sakila/model"
	optmodel "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/sqlite/test_sample/model"
	sakila "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/sqlite/sakila/table"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/sqlite/test_sample/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// filmWithOriginalLanguage is the nested destination of the NestedOptionStruct tests:
// a film with an optional original language, read through a LEFT JOIN.
type filmWithOriginalLanguage struct {
	optsakila.Film
	OriginalLanguage functional.Option[optsakila.Language] `alias:"original_language.*"`
}

func TestOptionNestedStruct(t *testing.T) {
	tx := beginDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := sakila.Film.UPDATE(sakila.Film.OriginalLanguageID).SET(Int(2)).
		WHERE(sakila.Film.FilmID.EQ(Int(1))).Exec(tx)
	require.NoError(t, err)

	originalLanguage := sakila.Language.AS("original_language")
	var films []filmWithOriginalLanguage
	err = SELECT(sakila.Film.AllColumns, originalLanguage.AllColumns).
		FROM(sakila.Film.LEFT_JOIN(originalLanguage, sakila.Film.OriginalLanguageID.EQ(originalLanguage.LanguageID))).
		WHERE(sakila.Film.FilmID.IN(Int(1), Int(2))).
		ORDER_BY(sakila.Film.FilmID).
		Query(tx, &films)
	require.NoError(t, err)

	require.Len(t, films, 2)
	lang, ok := films[0].OriginalLanguage.Get()
	require.True(t, ok)
	require.Equal(t, int16(2), lang.LanguageID)
	require.Equal(t, "Italian", lang.Name)
	require.Equal(t, functional.Some[int16](2), films[0].OriginalLanguageID)
	require.False(t, films[1].OriginalLanguage.IsPresent())

	var actors []struct {
		optsakila.Actor
		Films []filmWithOriginalLanguage
	}
	err = SELECT(sakila.Actor.AllColumns, sakila.Film.AllColumns, originalLanguage.AllColumns).
		FROM(sakila.Actor.
			INNER_JOIN(sakila.FilmActor, sakila.FilmActor.ActorID.EQ(sakila.Actor.ActorID)).
			INNER_JOIN(sakila.Film, sakila.Film.FilmID.EQ(sakila.FilmActor.FilmID)).
			LEFT_JOIN(originalLanguage, sakila.Film.OriginalLanguageID.EQ(originalLanguage.LanguageID))).
		WHERE(sakila.Actor.ActorID.EQ(Int(1))).
		ORDER_BY(sakila.Film.FilmID).
		Query(tx, &actors)
	require.NoError(t, err)

	require.Len(t, actors, 1)
	require.Greater(t, len(actors[0].Films), 1)
	require.Equal(t, int64(1), actors[0].Films[0].FilmID)
	require.True(t, actors[0].Films[0].OriginalLanguage.IsPresent())
	for _, f := range actors[0].Films[1:] {
		require.False(t, f.OriginalLanguage.IsPresent(), "film %d", f.FilmID)
	}
}

func TestOptionCustomDestination(t *testing.T) {
	// An anonymous destination: a one-part alias ("max_integer") then matches the
	// column alias as is. The group whose text_ptr and MAX are both NULL maps to no value,
	// so it is left out, as a row of all-NULL columns always is.
	var dest []struct {
		TextPtr    functional.Option[string] `alias:"all_types.text_ptr"`
		MaxInteger functional.Option[int64]  `alias:"max_integer"`
	}
	err := SELECT(sample.AllTypes.TextPtr, MAX(sample.AllTypes.IntegerPtr).AS("max_integer")).
		FROM(sample.AllTypes).
		GROUP_BY(sample.AllTypes.TextPtr).
		ORDER_BY(sample.AllTypes.TextPtr.ASC()).
		Query(sampleDB, &dest)
	require.NoError(t, err)
	require.Len(t, dest, 1)
	require.True(t, dest[0].TextPtr.IsPresent())
	require.True(t, dest[0].MaxInteger.IsPresent())

	var none struct {
		MaxInteger functional.Option[int64] `alias:"max_integer"`
	}
	err = SELECT(MAX(sample.AllTypes.IntegerPtr).AS("max_integer")).
		FROM(sample.AllTypes).
		WHERE(sample.AllTypes.Text.EQ(String("no such row"))).
		Query(sampleDB, &none)
	require.NoError(t, err)
	require.False(t, none.MaxInteger.IsPresent(), "MAX over nothing is NULL")

	// The Rows.Scan path.
	rows, err := SELECT(sample.AllTypes.AllColumns).FROM(sample.AllTypes).Rows(context.Background(), sampleDB)
	require.NoError(t, err)
	defer rows.Close()
	var scanned []optmodel.AllTypes
	for rows.Next() {
		var row optmodel.AllTypes
		require.NoError(t, rows.Scan(&row))
		scanned = append(scanned, row)
	}
	require.NoError(t, rows.Err())
	require.Len(t, scanned, 2)
	require.True(t, scanned[0].TextPtr.IsPresent())
	require.False(t, scanned[1].TextPtr.IsPresent())
}

type sqliteJSONPayload struct {
	A int    `json:"a"`
	B string `json:"b"`
}

// json_column with an Option field. The SQLite driver returns TEXT as string, and qrm's
// json_column mapping (upstream) reads []byte, so the JSON is cast to BLOB, as a *T
// field needs too. A NULL JSON value is None, as it is nil for a *T field.
func TestOptionJSONColumn(t *testing.T) {
	var dest []struct {
		ID         int64                                `alias:"id" sql:"primary_key"`
		Payload    functional.Option[sqliteJSONPayload] `json_column:"payload"`
		PayloadPtr *sqliteJSONPayload                   `json_column:"payload"`
	}
	err := SELECT(Int(1).AS("id"), Raw("CAST(json_object('a', 1, 'b', 'x') AS BLOB)").AS("payload")).
		UNION_ALL(SELECT(Int(2), Raw("CAST(NULL AS BLOB)"))).
		Query(sampleDB, &dest)
	require.NoError(t, err)
	require.Len(t, dest, 2)
	require.Equal(t, functional.Some(sqliteJSONPayload{A: 1, B: "x"}), dest[0].Payload)
	require.Equal(t, &sqliteJSONPayload{A: 1, B: "x"}, dest[0].PayloadPtr)
	require.False(t, dest[1].Payload.IsPresent())
	require.Nil(t, dest[1].PayloadPtr)
}
