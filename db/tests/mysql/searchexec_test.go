// This file is Nanostack-only (not in go-jet): the db/searchexec suite on MySQL and MariaDB.
// See db/UPSTREAM.md and db/tests/internal/searchexeccontract.

package mysql

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/db/keyset"
	"github.com/nanostack-dev/nanostack-framework/db/mysql"
	"github.com/nanostack-dev/nanostack-framework/db/postgres"
	"github.com/nanostack-dev/nanostack-framework/db/tests/dbconfig"
	"github.com/nanostack-dev/nanostack-framework/db/tests/internal/searchexeccontract"
)

func TestSearchexecContract(t *testing.T) {
	// The driver writes and reads times in UTC, in a UTC session.
	dsn := dbconfig.MySQLConnectionString(sourceIsMariaDB(), "test_sample") +
		"?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27"
	flavor := searchexeccontract.MySQL
	if sourceIsMariaDB() {
		flavor = searchexeccontract.MariaDB
	}
	searchexeccontract.Run(t, searchexeccontract.Config{
		Flavor: flavor,
		DB:     openKeysetDB(t, dsn),
		Schema: "test_sample",
		NewTable: func(schema, name string, columns ...postgres.Column) keyset.Source {
			return mysql.NewTable(schema, name, "", columns...)
		},
		InnerJoin: func(left, right keyset.Source, on postgres.BoolExpression) keyset.Source {
			return left.(mysql.Table).INNER_JOIN(right.(mysql.Table), on)
		},
	})
}
