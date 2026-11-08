// This file is Nanostack-only (not in go-jet): the db/query helpers against SQLite.
// See db/UPSTREAM.md.

package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	. "github.com/nanostack-dev/nanostack-framework/db/sqlite"
	optsakila "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/sqlite/sakila/model"
	sakila "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/sqlite/sakila/table"
)

// requireNoRowsNeverLeaks checks the db/query contract on every result of this file.
func requireNoRowsNeverLeaks(t *testing.T, err error) {
	t.Helper()
	require.NotErrorIs(t, err, sql.ErrNoRows, "sql.ErrNoRows leaked: %v", err)
	require.NotErrorIs(t, err, qrm.ErrNoRows, "qrm.ErrNoRows leaked: %v", err)
}

func selectActors(where BoolExpression) SelectStatement {
	return SELECT(sakila.Actor.AllColumns).FROM(sakila.Actor).WHERE(where).ORDER_BY(sakila.Actor.ActorID)
}

func TestQueryOne(t *testing.T) {
	ctx := context.Background()

	none, err := query.One[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.EQ(Int(-1))))
	require.NoError(t, err)
	require.False(t, none.IsPresent())

	one, err := query.One[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "PENELOPE", one.Value().FirstName)

	_, err = query.One[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.IN(Int(1), Int(2))))
	require.ErrorIs(t, err, query.ErrTooManyRows)
	requireNoRowsNeverLeaks(t, err)

	type actorWithFilms struct {
		optsakila.Actor
		Films []optsakila.Film
	}
	withFilms, err := query.One[actorWithFilms](ctx, db,
		SELECT(sakila.Actor.AllColumns, sakila.Film.AllColumns).
			FROM(sakila.Actor.
				INNER_JOIN(sakila.FilmActor, sakila.FilmActor.ActorID.EQ(sakila.Actor.ActorID)).
				INNER_JOIN(sakila.Film, sakila.Film.FilmID.EQ(sakila.FilmActor.FilmID))).
			WHERE(sakila.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Greater(t, len(withFilms.Value().Films), 1, "many rows of one actor are one value")

	count, err := query.One[int64](ctx, db, SELECT(COUNT(STAR)).FROM(sakila.Actor))
	require.NoError(t, err)
	require.Equal(t, int64(200), count.Value())

	maxOfNothing, err := query.One[int64](ctx, db,
		SELECT(MAX(sakila.Actor.ActorID)).FROM(sakila.Actor).WHERE(sakila.Actor.ActorID.LT(Int(0))))
	require.NoError(t, err)
	require.False(t, maxOfNothing.IsPresent())

	name, err := query.OneMap(ctx, db, selectActors(sakila.Actor.ActorID.EQ(Int(1))),
		func(a optsakila.Actor) string { return a.LastName })
	require.NoError(t, err)
	require.Equal(t, "GUINESS", name.Value())
}

func TestQueryUpdateReturningNoMatch(t *testing.T) {
	tx := beginDBTx(t)
	defer func() { _ = tx.Rollback() }()

	got, err := query.One[optsakila.Actor](context.Background(), tx,
		sakila.Actor.UPDATE(sakila.Actor.FirstName).SET(String("X")).
			WHERE(sakila.Actor.ActorID.EQ(Int(-1))).
			RETURNING(sakila.Actor.AllColumns))
	require.NoError(t, err)
	require.False(t, got.IsPresent())
}

func TestQueryAll(t *testing.T) {
	ctx := context.Background()

	empty, err := query.All[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.LT(Int(0))))
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)

	many, err := query.All[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.LT_EQ(Int(3))))
	require.NoError(t, err)
	require.Len(t, many, 3)

	ids, err := query.AllMap(ctx, db, selectActors(sakila.Actor.ActorID.LT_EQ(Int(3))),
		func(a optsakila.Actor) int64 { return a.ActorID })
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, ids.ToSlice())
}

func TestQueryExecAndCount(t *testing.T) {
	ctx := context.Background()
	tx := beginDBTx(t)
	defer func() { _ = tx.Rollback() }()

	res, err := query.Exec(ctx, tx, sakila.Actor.UPDATE(sakila.Actor.FirstName).SET(String("X")).
		WHERE(sakila.Actor.ActorID.LT_EQ(Int(2))))
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)

	n, err := query.Count(ctx, tx, SELECT(COUNT(STAR)).FROM(sakila.Actor).WHERE(sakila.Actor.FirstName.EQ(String("X"))))
	require.NoError(t, err)
	require.Equal(t, int64(2), n)

	n, err = query.Count(ctx, tx, SELECT(COUNT(STAR)).FROM(sakila.Actor).
		WHERE(sakila.Actor.ActorID.LT(Int(0))).GROUP_BY(sakila.Actor.ActorID))
	require.NoError(t, err)
	require.Zero(t, n, "COUNT ... GROUP BY with no groups")
}

func TestQueryDriverError(t *testing.T) {
	tx := beginDBTx(t)
	defer func() { _ = tx.Rollback() }()

	_, err := query.Exec(
		context.Background(),
		tx,
		sakila.Actor.INSERT(sakila.Actor.ActorID, sakila.Actor.FirstName, sakila.Actor.LastName).
			VALUES(1, "dup", "dup"),
	)
	require.Error(t, err)
	var driverErr sqlite3.Error
	require.ErrorAs(t, err, &driverErr)
	require.Equal(t, sqlite3.ErrConstraint, driverErr.Code)
	requireNoRowsNeverLeaks(t, err)
}

func TestQueryInTransaction(t *testing.T) {
	ctx := context.Background()
	tx := beginDBTx(t)

	_, err := query.Exec(ctx, tx, sakila.Actor.UPDATE(sakila.Actor.FirstName).SET(String("IN-TX")).
		WHERE(sakila.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)

	inTx, err := query.One[optsakila.Actor](ctx, tx, selectActors(sakila.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "IN-TX", inTx.Value().FirstName)

	require.NoError(t, tx.Rollback())

	after, err := query.One[optsakila.Actor](ctx, db, selectActors(sakila.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "PENELOPE", after.Value().FirstName)
}
