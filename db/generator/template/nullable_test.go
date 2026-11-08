// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package template

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/generator/metadata"
)

func TestNullableStrategies(t *testing.T) {
	cases := []struct {
		name     string
		in       Type
		pointer  Type
		optional Type
	}{
		{"builtin", NewType(int32(0)),
			Type{Name: "*int32"},
			Type{Name: "functional.Option[int32]", ImportPath: FunctionalImportPath}},
		{
			"time",
			NewType(time.Time{}),
			Type{Name: "*time.Time", ImportPath: "time"},
			Type{
				Name:                  "functional.Option[time.Time]",
				ImportPath:            FunctionalImportPath,
				AdditionalImportPaths: []string{"time"},
			},
		},
		{
			"decimal with additional imports",
			Type{
				Name:                  "decimal.Decimal",
				ImportPath:            "github.com/shopspring/decimal",
				AdditionalImportPaths: []string{"x/y"},
			},
			Type{
				Name:                  "*decimal.Decimal",
				ImportPath:            "github.com/shopspring/decimal",
				AdditionalImportPaths: []string{"x/y"},
			},
			Type{
				Name:                  "functional.Option[decimal.Decimal]",
				ImportPath:            FunctionalImportPath,
				AdditionalImportPaths: []string{"github.com/shopspring/decimal", "x/y"},
			},
		},
		{"enum in the same package", Type{Name: "Mood"},
			Type{Name: "*Mood"},
			Type{Name: "functional.Option[Mood]", ImportPath: FunctionalImportPath}},
		{
			"array",
			NewType(pq.StringArray{}),
			Type{Name: "*pq.StringArray", ImportPath: "github.com/lib/pq"},
			Type{
				Name:                  "functional.Option[pq.StringArray]",
				ImportPath:            FunctionalImportPath,
				AdditionalImportPaths: []string{"github.com/lib/pq"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.pointer, NullablePointer(c.in))
			require.Equal(t, c.optional, NullableOption(c.in))
		})
	}
}

var (
	nullableSmallint = metadata.Column{Name: "small_int_ptr", IsNullable: true,
		DataType: metadata.DataType{Name: "smallint", Kind: metadata.BaseType}}
	notNullTimestamp = metadata.Column{Name: "created_at",
		DataType: metadata.DataType{Name: "timestamp with time zone", Kind: metadata.BaseType}}
	nullablePK = metadata.Column{Name: "id", IsPrimaryKey: true, IsNullable: true,
		DataType: metadata.DataType{Name: "bigint", Kind: metadata.BaseType}}
	nullableEnum = metadata.Column{Name: "mood_ptr", IsNullable: true,
		DataType: metadata.DataType{Name: "mood", Kind: metadata.EnumType}}
	nullableTextArray = metadata.Column{Name: "text_array_ptr", IsNullable: true,
		DataType: metadata.DataType{Name: "text", Kind: metadata.BaseType, Dimensions: 1}}
	nullableEnumArray = metadata.Column{Name: "mood_array_ptr", IsNullable: true,
		DataType: metadata.DataType{Name: "mood", Kind: metadata.EnumType, Dimensions: 1}}
)

func TestDefaultTableModelFieldWith(t *testing.T) {
	option := DefaultTableModelFieldWith(NullableOption)

	require.Equal(t, TableModelField{Name: "SmallIntPtr",
		Type: Type{Name: "functional.Option[int16]", ImportPath: FunctionalImportPath}}, option(nullableSmallint))
	require.Equal(t, TableModelField{Name: "CreatedAt",
		Type: Type{Name: "time.Time", ImportPath: "time"}}, option(notNullTimestamp))
	require.Equal(t, TableModelField{Name: "ID", Tags: []string{`sql:"primary_key"`},
		Type: Type{Name: "functional.Option[int64]", ImportPath: FunctionalImportPath}}, option(nullablePK))
	require.Equal(t, TableModelField{Name: "MoodPtr",
		Type: Type{Name: "functional.Option[Mood]", ImportPath: FunctionalImportPath}}, option(nullableEnum))
	require.Equal(t, TableModelField{
		Name: "TextArrayPtr",
		Type: Type{
			Name:                  "functional.Option[pq.StringArray]",
			ImportPath:            FunctionalImportPath,
			AdditionalImportPaths: []string{"github.com/lib/pq"},
		},
	},
		option(nullableTextArray))
	require.Equal(t, TableModelField{
		Name: "MoodArrayPtr",
		Type: Type{
			Name:                  "functional.Option[pq.StringArray]",
			ImportPath:            FunctionalImportPath,
			AdditionalImportPaths: []string{"github.com/lib/pq"},
		},
	},
		option(nullableEnumArray))

	// The pointer strategy is upstream's DefaultTableModelField.
	pointer := DefaultTableModelFieldWith(NullablePointer)
	for _, c := range []metadata.Column{nullableSmallint, notNullTimestamp, nullablePK, nullableEnum, nullableTextArray, nullableEnumArray} {
		require.Equal(t, DefaultTableModelField(c), pointer(c), c.Name)
	}
	require.Equal(t, Type{Name: "*int16"}, DefaultTableModelField(nullableSmallint).Type)
	require.Equal(t, Type{Name: "*Mood"}, DefaultTableModelField(nullableEnum).Type)
	require.Equal(
		t,
		Type{Name: "*pq.StringArray", ImportPath: "github.com/lib/pq"},
		DefaultTableModelField(nullableEnumArray).Type,
	)
}

func TestRenderOptionModel(t *testing.T) {
	schema := metadata.Schema{
		Name: "sample",
		TablesMetaData: []metadata.Table{{
			Name:    "all_types",
			Columns: []metadata.Column{nullablePK, nullableSmallint, notNullTimestamp, nullableEnum, nullableTextArray},
		}},
		EnumsMetaData: []metadata.Enum{{Name: "mood", Values: []string{"happy"}}},
	}
	dir := t.TempDir()
	tmpl := DefaultWith(nil, NullableOption).UseSchema(func(s metadata.Schema) Schema {
		return DefaultSchemaWith(NullableOption)(s).UseSQLBuilder(DefaultSQLBuilder().ShouldSkip(true))
	})
	require.NoError(t, ProcessSchema(dir, schema, tmpl))

	got, err := os.ReadFile(filepath.Join(dir, "sample", "model", "all_types.go"))
	require.NoError(t, err)
	require.Equal(t, `//
// Code generated by go-jet DO NOT EDIT.
//
// WARNING: Changes to this file may cause incorrect behavior
// and will be lost if the code is regenerated
//

package model

import (
	"github.com/lib/pq"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
	"time"
)

type AllTypes struct {
	ID           functional.Option[int64] `+"`sql:\"primary_key\"`"+`
	SmallIntPtr  functional.Option[int16]
	CreatedAt    time.Time
	MoodPtr      functional.Option[Mood]
	TextArrayPtr functional.Option[pq.StringArray]
}
`, string(got))
}
