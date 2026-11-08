//go:build postgres

// This file is Nanostack-only (not in go-jet): functional.Option over PostgreSQL range
// types. Like upstream's range_test.go it is behind the postgres build tag: CockroachDB has
// no range types, and its generated models have no sample_ranges table. See db/UPSTREAM.md.

package postgres

import (
	"testing"

	"github.com/jackc/pgtype"
	"github.com/stretchr/testify/require"

	. "github.com/nanostack-dev/nanostack-framework/db/postgres"
	sample "github.com/nanostack-dev/nanostack-framework/db/tests/.gentestdata/jetdb/test_sample/table"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

func TestOptionPostgresRange(t *testing.T) {
	skipForCockroachDB(t) // no range types

	var dest struct {
		Int4Range functional.Option[pgtype.Int4range] `alias:"sample_ranges.int4_range"`
		NoRange   functional.Option[pgtype.Int4range] `alias:"no_range"`
	}
	err := SELECT(sample.SampleRanges.Int4Range, Raw("NULL::int4range").AS("no_range")).
		FROM(sample.SampleRanges).
		LIMIT(1).
		Query(db, &dest)
	require.NoError(t, err)
	r, ok := dest.Int4Range.Get()
	require.True(t, ok)
	require.Equal(t, pgtype.Present, r.Status)
	require.False(t, dest.NoRange.IsPresent())
}
