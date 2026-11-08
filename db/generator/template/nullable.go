// This file is Nanostack-only (not in go-jet): the Go type of nullable columns.
// See db/UPSTREAM.md.

package template

import (
	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
	"github.com/nanostack-dev/nanostack-framework/db/internal/jet"
)

// Nullable maps the Go type of a column's non-NULL values to the type of a nullable
// column's field.
type Nullable func(nonNull Type) Type

// FunctionalImportPath is the import path written into Option-mode models.
const FunctionalImportPath = "github.com/nanostack-dev/nanostack-framework/pkg/functional"

// NullablePointer is upstream go-jet's behaviour: a nullable column is a *T field.
func NullablePointer(t Type) Type {
	t.Name = "*" + t.Name
	return t
}

// NullableOption types a nullable column as functional.Option[T]. NULL reads as None,
// and None writes NULL.
func NullableOption(t Type) Type {
	var additional []string
	if t.ImportPath != "" {
		additional = append(additional, t.ImportPath)
	}
	additional = append(additional, t.AdditionalImportPaths...)

	return Type{
		Name:                  "functional.Option[" + t.Name + "]",
		ImportPath:            FunctionalImportPath,
		AdditionalImportPaths: additional,
	}
}

// DefaultTableModelFieldWith is DefaultTableModelField with nullable columns typed by
// nullable. Tags, field name and the column tag logic are the upstream ones.
func DefaultTableModelFieldWith(nullable Nullable) func(metadata.Column) TableModelField {
	return func(column metadata.Column) TableModelField {
		field := DefaultTableModelField(column)
		field.Type = getType(column, nullable)
		return field
	}
}

// DefaultTableModelWith is DefaultTableModel with nullable columns typed by nullable.
func DefaultTableModelWith(nullable Nullable) func(metadata.Table) TableModel {
	return func(table metadata.Table) TableModel {
		return DefaultTableModel(table).UseField(DefaultTableModelFieldWith(nullable))
	}
}

// DefaultModelWith is DefaultModel with table and view models typed by nullable.
func DefaultModelWith(nullable Nullable) Model {
	tableModel := DefaultTableModelWith(nullable)
	return DefaultModel().UseTable(tableModel).UseView(tableModel)
}

// DefaultSchemaWith is DefaultSchema with models typed by nullable.
func DefaultSchemaWith(nullable Nullable) func(metadata.Schema) Schema {
	return func(schema metadata.Schema) Schema {
		return DefaultSchema(schema).UseModel(DefaultModelWith(nullable))
	}
}

// DefaultWith is Default with nullable columns typed by nullable:
//
//	template.DefaultWith(postgres.Dialect, template.NullableOption)
func DefaultWith(dialect jet.Dialect, nullable Nullable) Template {
	return Default(dialect).UseSchema(DefaultSchemaWith(nullable))
}
