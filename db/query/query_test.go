package query_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

var ctx = context.Background()

type Actor struct {
	ActorID int64 `sql:"primary_key"`
	Name    string
}

type Film struct {
	FilmID int64 `sql:"primary_key"`
}

type ActorWithFilms struct {
	Actor
	Films []Film
}

var (
	actorColumns = []string{"actor.actor_id", "actor.name"}
	stmt         = RawStatement("SELECT whatever")
)

func TestOne(t *testing.T) {
	t.Run("no row is None", func(t *testing.T) {
		got, err := query.One[Actor](ctx, openFake(script{columns: actorColumns}), stmt)
		require.NoError(t, err)
		require.False(t, got.IsPresent())
	})

	t.Run("one row is Some", func(t *testing.T) {
		got, err := query.One[Actor](
			ctx,
			openFake(script{columns: actorColumns, rows: [][]driver.Value{{int64(1), "Ann"}}}),
			stmt,
		)
		require.NoError(t, err)
		require.Equal(t, functional.Some(Actor{ActorID: 1, Name: "Ann"}), got)
	})

	t.Run("two objects are ErrTooManyRows", func(t *testing.T) {
		got, err := query.One[Actor](ctx, openFake(script{columns: actorColumns,
			rows: [][]driver.Value{{int64(1), "Ann"}, {int64(2), "Bob"}}}), stmt)
		require.ErrorIs(t, err, query.ErrTooManyRows)
		require.EqualError(
			t,
			err,
			"query.One[query_test.Actor]: query: statement returned more than one result (2 results)",
		)
		require.False(t, got.IsPresent())
	})

	t.Run("grouped rows of one parent are one object", func(t *testing.T) {
		got, err := query.One[ActorWithFilms](ctx, openFake(script{
			columns: []string{"actor.actor_id", "actor.name", "film.film_id"},
			rows:    [][]driver.Value{{int64(1), "Ann", int64(10)}, {int64(1), "Ann", int64(11)}},
		}), stmt)
		require.NoError(t, err)
		require.Equal(
			t,
			functional.Some(ActorWithFilms{ActorID: 1, Name: "Ann", Films: []Film{{10}, {11}}}),
			got,
		)
	})

	t.Run("scalar", func(t *testing.T) {
		got, err := query.One[int64](
			ctx,
			openFake(script{columns: []string{"count"}, rows: [][]driver.Value{{int64(3)}}}),
			stmt,
		)
		require.NoError(t, err)
		require.Equal(t, functional.Some[int64](3), got)

		got, err = query.One[int64](
			ctx,
			openFake(script{columns: []string{"max"}, rows: [][]driver.Value{{nil}}}),
			stmt,
		)
		require.NoError(t, err)
		require.False(t, got.IsPresent(), "MAX over nothing is NULL, which is None")
	})

	t.Run("all-NULL row is None", func(t *testing.T) {
		got, err := query.One[Actor](
			ctx,
			openFake(script{columns: actorColumns, rows: [][]driver.Value{{nil, nil}}}),
			stmt,
		)
		require.NoError(t, err)
		require.False(t, got.IsPresent())
	})

	t.Run("a no-rows error from the driver is None", func(t *testing.T) {
		for _, noRows := range []error{sql.ErrNoRows, qrm.ErrNoRows} {
			got, err := query.One[Actor](ctx, openFake(script{err: noRows}), stmt)
			require.NoError(t, err)
			require.False(t, got.IsPresent())
		}
	})

	t.Run("a driver error is wrapped once by query (qrm adds its jet: prefix)", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := query.One[Actor](ctx, openFake(script{err: boom}), stmt)
		require.ErrorIs(t, err, boom)
		require.EqualError(t, err, "query.One[query_test.Actor]: jet: boom")
	})
}

func TestOneMap(t *testing.T) {
	got, err := query.OneMap(
		ctx,
		openFake(script{columns: actorColumns, rows: [][]driver.Value{{int64(1), "Ann"}}}),
		stmt,
		func(a Actor) string { return a.Name },
	)
	require.NoError(t, err)
	require.Equal(t, functional.Some("Ann"), got)

	_, err = query.OneMap(ctx, openFake(script{err: errors.New("boom")}), stmt, func(a Actor) string { return a.Name })
	require.EqualError(t, err, "query.One[query_test.Actor]: jet: boom")
}

func TestAll(t *testing.T) {
	got, err := query.All[Actor](ctx, openFake(script{columns: actorColumns}), stmt)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)

	got, err = query.All[Actor](ctx, openFake(script{columns: actorColumns,
		rows: [][]driver.Value{{int64(1), "Ann"}, {int64(2), "Bob"}}}), stmt)
	require.NoError(t, err)
	require.Equal(t, functional.SeqOf(Actor{1, "Ann"}, Actor{2, "Bob"}), got)

	got, err = query.All[Actor](ctx, openFake(script{err: sql.ErrNoRows}), stmt)
	require.NoError(t, err)
	require.NotNil(t, got)

	_, err = query.All[Actor](ctx, openFake(script{err: errors.New("boom")}), stmt)
	require.EqualError(t, err, "query.All[query_test.Actor]: jet: boom")
}

func TestAllMap(t *testing.T) {
	got, err := query.AllMap(ctx, openFake(script{columns: actorColumns,
		rows: [][]driver.Value{{int64(1), "Ann"}, {int64(2), "Bob"}}}), stmt, func(a Actor) int64 { return a.ActorID })
	require.NoError(t, err)
	require.Equal(t, functional.SeqOf[int64](1, 2), got)

	got, err = query.AllMap(
		ctx,
		openFake(script{columns: actorColumns}),
		stmt,
		func(a Actor) int64 { return a.ActorID },
	)
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestExec(t *testing.T) {
	res, err := query.Exec(ctx, openFake(script{rowsAffected: 3}), stmt)
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	boom := errors.New("boom")
	_, err = query.Exec(ctx, openFake(script{err: boom}), stmt)
	require.ErrorIs(t, err, boom)
	require.EqualError(t, err, "query.Exec: boom")
}

func TestCount(t *testing.T) {
	n, err := query.Count(ctx, openFake(script{columns: []string{"count"}, rows: [][]driver.Value{{int64(7)}}}), stmt)
	require.NoError(t, err)
	require.Equal(t, int64(7), n)

	n, err = query.Count(ctx, openFake(script{columns: []string{"count"}}), stmt)
	require.NoError(t, err)
	require.Zero(t, n, "COUNT ... GROUP BY with no groups gives no row")

	_, err = query.Count(
		ctx,
		openFake(script{columns: []string{"count"}, rows: [][]driver.Value{{int64(1)}, {int64(2)}}}),
		stmt,
	)
	require.ErrorIs(t, err, query.ErrTooManyRows)
	require.EqualError(t, err, "query.Count: query: statement returned more than one result (2 results)")
}

func TestSelectJsonObjIsNotSupported(t *testing.T) {
	jsonObj := SELECT_JSON_OBJ(Int(1).AS("a"))
	require.PanicsWithValue(
		t,
		"jet: SELECT_JSON_OBJ destination has to be a pointer to struct or pointer to map[string]any",
		func() {
			_, _ = query.One[Actor](ctx, openFake(script{}), jsonObj)
		},
	)
}
