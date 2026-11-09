// Modified by Nanostack from go-jet (https://github.com/go-jet/jet), commit d0e152c.
// See db/UPSTREAM.md for the upstream base and the list of changes.

package metadata

// Column struct
type Column struct {
	Name         string `sql:"primary_key"`
	IsPrimaryKey bool
	IsNullable   bool
	IsGenerated  bool
	HasDefault   bool
	DataType     DataType
	Comment      string
	// CatalogType is the type name of the column in the catalogue (Nanostack, see catalog.go).
	CatalogType string
}

// DataTypeKind is database type kind(base, enum, user-defined, array)
type DataTypeKind string

// DataTypeKind possible values
const (
	BaseType        DataTypeKind = "base"
	EnumType        DataTypeKind = "enum"
	UserDefinedType DataTypeKind = "user-defined"
	RangeType       DataTypeKind = "range"
)

// DataType contains information about column data type
type DataType struct {
	Name          string
	Kind          DataTypeKind
	IsUnsigned    bool
	Dimensions    int // The number of array dimensions
	SourceDialect string
}

func (d DataType) IsArray() bool {
	return d.Dimensions > 0
}
