// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
	"github.com/nanostack-dev/nanostack-framework/db/generator/template"
	"github.com/nanostack-dev/nanostack-framework/db/sqlite"
)

// GenerateDSN generates jet files using dsn connection string
func GenerateDSN(dsn, destDir string, templates ...template.Template) error {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("failed to open sqlite connection: %w", err)
	}
	defer db.Close()

	fmt.Println("Retrieving schema information...")
	return GenerateDB(db, destDir, templates...)
}

// GenerateDB generates jet files using the provided *sql.DB
func GenerateDB(db *sql.DB, destDir string, templates ...template.Template) error {
	generatorTemplate := template.Default(sqlite.Dialect)
	if len(templates) > 0 {
		generatorTemplate = templates[0]
	}

	schemaMetadata, err := metadata.GetSchema(db, &sqliteQuerySet{}, "")
	if err != nil {
		return fmt.Errorf("failed to query database metadata: %w", err)
	}

	err = template.ProcessSchema(destDir, schemaMetadata, generatorTemplate)
	if err != nil {
		return fmt.Errorf("failed to process database %s: %w", schemaMetadata.Name, err)
	}

	return nil
}
