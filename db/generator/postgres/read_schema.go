package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

// ReadSchema reads the metadata of schema from db, as GenerateDB does, and
// prints nothing. Each base table also carries its unique indexes, its
// foreign keys and the catalogue type of each column, which GenerateDB does
// not read. Pass the result to template.Render to generate
// the files in memory. It needs PostgreSQL: CockroachDB has no array slicing
// for the index query.
func ReadSchema(db *sql.DB, schema string) (metadata.Schema, error) {
	schemaMetadata, err := metadata.ReadSchema(db, &postgresQuerySet{}, schema)
	if err != nil {
		return metadata.Schema{}, fmt.Errorf("failed to get '%s' schema metadata: %w", schema, err)
	}
	if err = addUniqueIndexes(db, schema, schemaMetadata.TablesMetaData); err != nil {
		return metadata.Schema{}, err
	}
	ctx := context.Background()
	if err = addCatalogTypes(ctx, db, schema, schemaMetadata.TablesMetaData); err != nil {
		return metadata.Schema{}, err
	}
	if err = addForeignKeys(ctx, db, schema, schemaMetadata.TablesMetaData); err != nil {
		return metadata.Schema{}, err
	}
	return schemaMetadata, nil
}
