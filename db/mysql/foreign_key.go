package mysql

import "github.com/nanostack-dev/nanostack-framework/db/internal/jet"

// ForeignKey is one foreign key of a table: the return type of the
// ForeignKeys method that nano gen db writes on each table type.
type ForeignKey = jet.ForeignKey
