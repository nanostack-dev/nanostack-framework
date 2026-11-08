// This file is Nanostack-only (not in go-jet): the db/query helpers against MySQL and
// MariaDB. See db/UPSTREAM.md.

package mysql

import (
	"database/sql"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/qrm"
	"github.com/nanostack-dev/nanostack-framework/db/query"
	dvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/mysql/dvds/table"
	optdvds "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/option/mysql/dvds/model"
)

// requireNoRowsNeverLeaks checks the db/query contract on every result of this file.
func requireNoRowsNeverLeaks(t *testing.T, err error) {
	t.Helper()
	require.NotErrorIs(t, err, sql.ErrNoRows, "sql.ErrNoRows leaked: %v", err)
	require.NotErrorIs(t, err, qrm.ErrNoRows, "qrm.ErrNoRows leaked: %v", err)
}

func selectActors(where BoolExpression) SelectStatement {
	return SELECT(dvds.Actor.AllColumns).FROM(dvds.Actor).WHERE(where).ORDER_BY(dvds.Actor.ActorID)
}

func TestQueryOne(t *testing.T) {
	none, err := query.One[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.EQ(Int(-1))))
	require.NoError(t, err)
	require.False(t, none.IsPresent())

	one, err := query.One[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "PENELOPE", one.Value().FirstName)

	_, err = query.One[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.IN(Int(1), Int(2))))
	require.ErrorIs(t, err, query.ErrTooManyRows)
	requireNoRowsNeverLeaks(t, err)

	type actorWithFilms struct {
		optdvds.Actor
		Films []optdvds.Film
	}
	withFilms, err := query.One[actorWithFilms](ctx, db,
		SELECT(dvds.Actor.AllColumns, dvds.Film.AllColumns).
			FROM(dvds.Actor.
				INNER_JOIN(dvds.FilmActor, dvds.FilmActor.ActorID.EQ(dvds.Actor.ActorID)).
				INNER_JOIN(dvds.Film, dvds.Film.FilmID.EQ(dvds.FilmActor.FilmID))).
			WHERE(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Greater(t, len(withFilms.Value().Films), 1, "many rows of one actor are one value")

	count, err := query.One[int64](ctx, db, SELECT(COUNT(STAR)).FROM(dvds.Actor))
	require.NoError(t, err)
	require.Equal(t, int64(200), count.Value())

	maxOfNothing, err := query.One[int64](ctx, db,
		SELECT(MAX(dvds.Actor.ActorID)).FROM(dvds.Actor).WHERE(dvds.Actor.ActorID.LT(Int(0))))
	require.NoError(t, err)
	require.False(t, maxOfNothing.IsPresent())

	name, err := query.OneMap(ctx, db, selectActors(dvds.Actor.ActorID.EQ(Int(1))),
		func(a optdvds.Actor) string { return a.LastName })
	require.NoError(t, err)
	require.Equal(t, "GUINESS", name.Value())
}

func TestQueryDeleteReturningNoMatch(t *testing.T) {
	if !sourceIsMariaDB() {
		t.Skip("MySQL has no RETURNING")
	}
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	got, err := query.One[optdvds.Actor](ctx, tx,
		dvds.Actor.DELETE().WHERE(dvds.Actor.ActorID.EQ(Int(0))).RETURNING(dvds.Actor.AllColumns))
	require.NoError(t, err)
	require.False(t, got.IsPresent())
}

func TestQueryAll(t *testing.T) {
	empty, err := query.All[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.LT(Int(0))))
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)

	many, err := query.All[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.LT_EQ(Int(3))))
	require.NoError(t, err)
	require.Len(t, many, 3)

	ids, err := query.AllMap(ctx, db, selectActors(dvds.Actor.ActorID.LT_EQ(Int(3))),
		func(a optdvds.Actor) uint16 { return a.ActorID })
	require.NoError(t, err)
	require.Equal(t, []uint16{1, 2, 3}, ids.ToSlice())
}

func TestQueryExecAndCount(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	res, err := query.Exec(ctx, tx, dvds.Actor.UPDATE(dvds.Actor.FirstName).SET(String("X")).
		WHERE(dvds.Actor.ActorID.LT_EQ(Int(2))))
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)

	n, err := query.Count(ctx, tx, SELECT(COUNT(STAR)).FROM(dvds.Actor).WHERE(dvds.Actor.FirstName.EQ(String("X"))))
	require.NoError(t, err)
	require.Equal(t, int64(2), n)

	n, err = query.Count(ctx, tx, SELECT(COUNT(STAR)).FROM(dvds.Actor).
		WHERE(dvds.Actor.ActorID.LT(Int(0))).GROUP_BY(dvds.Actor.ActorID))
	require.NoError(t, err)
	require.Zero(t, n, "COUNT ... GROUP BY with no groups")
}

func TestQueryDriverError(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	existing, err := query.One[optdvds.Actor](ctx, tx, selectActors(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	_, err = query.Exec(ctx, tx, dvds.Actor.INSERT(dvds.Actor.AllColumns).MODEL(existing.Value()))
	require.Error(t, err)
	requireNoRowsNeverLeaks(t, err)
	var myErr *mysql.MySQLError
	require.ErrorAs(t, err, &myErr)
	require.Equal(t, uint16(1062), myErr.Number, "duplicate entry")
}

func TestQueryInTransaction(t *testing.T) {
	tx, err := db.Begin()
	require.NoError(t, err)

	_, err = query.Exec(ctx, tx, dvds.Actor.UPDATE(dvds.Actor.FirstName).SET(String("IN-TX")).
		WHERE(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)

	inTx, err := query.One[optdvds.Actor](ctx, tx, selectActors(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "IN-TX", inTx.Value().FirstName)

	require.NoError(t, tx.Rollback())

	after, err := query.One[optdvds.Actor](ctx, db, selectActors(dvds.Actor.ActorID.EQ(Int(1))))
	require.NoError(t, err)
	require.Equal(t, "PENELOPE", after.Value().FirstName)
}
