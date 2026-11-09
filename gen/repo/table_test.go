package genrepo

import "testing"

// A column gets Contains and EqualsIgnoreCase only when the catalogue type
// in ColumnTypes is text: a model string is not enough, because inet, jsonb
// and the other types the model reads as a string fail under LIKE and LOWER.
func TestTextual(t *testing.T) {
	t.Parallel()
	text := column{field: "Name", lit: litString, dbType: "text"}
	for _, tc := range []struct {
		name string
		col  column
		want bool
	}{
		{"text", text, true},
		{"varchar", column{field: "Name", lit: litString, dbType: "varchar"}, true},
		{"char", column{field: "Name", lit: litString, dbType: "bpchar"}, true},
		{"citext", column{field: "Name", lit: litString, dbType: "citext"}, true},
		{"citext of an extension schema", column{field: "Name", lit: litString, dbType: "extensions.citext"}, true},
		{"inet", column{field: "Name", lit: litString, dbType: "inet"}, false},
		{"cidr", column{field: "Name", lit: litString, dbType: "cidr"}, false},
		{"jsonb", column{field: "Name", lit: litString, dbType: "jsonb"}, false},
		{"json", column{field: "Name", lit: litString, dbType: "json"}, false},
		{"xml", column{field: "Name", lit: litString, dbType: "xml"}, false},
		{"tsvector", column{field: "Name", lit: litString, dbType: "tsvector"}, false},
		{"uuid read as a string", column{field: "Name", lit: litString, dbType: "uuid"}, false},
		{"a user type named text", column{field: "Name", lit: litString, dbType: "public.text"}, false},
		{"no catalogue type", column{field: "Name", lit: litString}, false},
		{"primary key", column{field: "Name", lit: litString, dbType: "text", primary: true}, false},
		{"identifier", column{field: "ItemID", lit: litString, dbType: "text"}, false},
		{"enum", column{field: "Name", lit: litEnum, dbType: "item_status"}, false},
		{"text array", column{field: "Name", lit: litNone, dbType: "_text"}, false},
	} {
		if got := tc.col.textual(); got != tc.want {
			t.Errorf("%s: textual() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
