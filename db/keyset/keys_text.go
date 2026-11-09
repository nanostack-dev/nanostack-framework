package keyset

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"

	ijet "github.com/nanostack-dev/nanostack-framework/db/internal/jet"
	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// StringKey orders on a NOT NULL text column. The order and the matching
// follow the column's collation.
func StringKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return stringKey(column, false, ptrOf(value))
}

// NullableStringKey orders on a nullable text column.
func NullableStringKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return stringKey(column, true, value)
}

// textCodec refuses a NUL byte on PostgreSQL, whose text cannot hold it:
// binding it would fail in the database and not as an invalid cursor. MySQL
// and SQLite text holds NUL.
func textCodec() keyCodec[string] {
	return keyCodec[string]{
		encode: func(v string) any { return v },
		decode: func(raw json.RawMessage) (string, error) {
			var v string
			err := json.Unmarshal(raw, &v)
			return v, err
		},
		accept: func(d dialect, v string) error {
			if d == dialectPostgres && strings.ContainsRune(v, 0) {
				return errors.New("text contains NUL")
			}
			return nil
		},
	}
}

// textLiteral is a text literal: a cast to text on PostgreSQL, a bound string
// on MySQL and SQLite.
func textLiteral(d dialect, v string) ijet.Expression {
	if d == dialectPostgres {
		return jet.String(v)
	}
	return ijet.String(v)
}

func stringKey[T any](column jet.ColumnString, nullable bool, value func(T) *string) Key[T] {
	return newKey(column, nullable, value, textCodec(), literalBound(column, textLiteral))
}

// EnumKey orders on a NOT NULL enum column, in the declaration order of its
// labels. labels lists them in that order: a cursor that carries any other
// value is refused before the database sees it.
//
// On PostgreSQL the cursor value is cast to enumType, the enum's SQL type name,
// because PostgreSQL orders an enum by declaration order and refuses to
// compare it with text. enumType is written into the SQL as raw text, so it
// must match ^[a-z_][a-z0-9_.]*$ (for example "order_status" or
// "public.order_status"). EnumKey panics otherwise.
//
// On MySQL and MariaDB, ORDER BY uses the ENUM column itself, which sorts by
// the label's index, so labels must be the column's labels in the order of its
// definition. The seek is an IN list of the labels after the cursor's label,
// because MySQL compares an ENUM with a string as text. The empty string, the
// value MySQL stores for an invalid label (index 0), sorts before every label
// and is a valid cursor value.
//
// On SQLite the column is text. ORDER BY maps each label to its position with
// a CASE, and the seek is the same IN list as on MySQL. A value that is not a
// label sorts before every label, as on MySQL, and all such values tie.
//
// A text column that holds enum-like values and sorts as text is a StringKey.
func EnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) string) Key[T] {
	return enumKey(column, enumType, labels, false, ptrOf(value))
}

// NullableEnumKey orders on a nullable enum column. enumType is checked as in
// EnumKey.
func NullableEnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) *string) Key[T] {
	return enumKey(column, enumType, labels, true, value)
}

func enumKey[T any](
	column jet.ColumnString,
	enumType string,
	labels []string,
	nullable bool,
	value func(T) *string,
) Key[T] {
	mustIdentifier("enum type", enumType)
	allowed := slices.Clone(labels)
	codec := keyCodec[string]{
		encode: func(v string) any { return v },
		decode: func(raw json.RawMessage) (string, error) {
			var v string
			err := json.Unmarshal(raw, &v)
			return v, err
		},
		accept: func(d dialect, v string) error {
			switch {
			case slices.Contains(allowed, v):
				return nil
			case d == dialectMySQL && v == "":
				// MySQL's value for an invalid label, index 0.
				return nil
			case d == dialectSQLite:
				// SQLite text holds any value. It sorts before every label.
				return nil
			}
			return fmt.Errorf("not a %s label", enumType)
		},
	}
	key := newKey(column, nullable, value, codec, func(d dialect, v string) keyBound {
		if d == dialectPostgres {
			typed := ijet.StringExp(jet.CAST(jet.String(v)).AS(enumType))
			return columnBound(column, typed)
		}
		return enumListBound(column, allowed, v)
	})
	key.sortExpr = func(d dialect) ijet.Expression {
		if d != dialectSQLite {
			return column
		}
		// A searched CASE keeps NULL as NULL, so the NULL placement of the key
		// applies. A value that is not a label sorts at -1, before every label.
		position := ijet.CASE()
		for i, label := range allowed {
			position = position.WHEN(column.EQ(ijet.String(label))).THEN(ijet.Int(int64(i)))
		}
		return position.WHEN(column.IS_NOT_NULL()).THEN(ijet.Int(-1))
	}
	key.columnOrder = func(d dialect) bool { return d == dialectPostgres }
	return key
}

// enumListBound compares an enum column with the label v by position, through
// IN lists of labels, on MySQL and SQLite. A value that is not a label has the
// position before every label, and all such values tie.
func enumListBound(column jet.ColumnString, labels []string, v string) keyBound {
	in := func(subset []string) ijet.BoolExpression {
		if len(subset) == 0 {
			return nil
		}
		return column.IN(literals(subset)...)
	}
	all := in(labels)
	unknown := column.NOT_IN(literals(labels)...)
	position := slices.Index(labels, v)
	if position < 0 {
		return keyBound{gt: all, eq: unknown}
	}
	lt := unknown
	if before := in(labels[:position]); before != nil {
		lt = before.OR(unknown)
	}
	return keyBound{
		gt: in(labels[position+1:]),
		lt: lt,
		eq: column.EQ(ijet.String(v)),
	}
}

func literals(labels []string) []ijet.Expression {
	values := make([]ijet.Expression, len(labels))
	for i, label := range labels {
		values[i] = ijet.String(label)
	}
	return values
}

// InetKey orders on a NOT NULL PostgreSQL inet column, read as its text form.
// PostgreSQL refuses to compare inet with text, so the cursor value is cast to
// inet. A value that is not an address or a network, or an address with an
// IPv6 zone (fe80::1%eth0), which inet cannot hold, is refused before the
// cast. MySQL and SQLite have no inet type: Page returns ErrDialect.
func InetKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return inetKey(column, false, ptrOf(value))
}

// NullableInetKey orders on a nullable inet column.
func NullableInetKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return inetKey(column, true, value)
}

func inetKey[T any](column jet.ColumnString, nullable bool, value func(T) *string) Key[T] {
	text := textCodec()
	codec := keyCodec[string]{
		encode: text.encode,
		decode: func(raw json.RawMessage) (string, error) {
			v, err := text.decode(raw)
			if err != nil {
				return "", err
			}
			if strings.ContainsRune(v, 0) {
				return "", errors.New("text contains NUL")
			}
			return v, checkInet(v)
		},
	}
	key := newKey(column, nullable, value, codec, literalBound(column, func(_ dialect, v string) ijet.Expression {
		return ijet.StringExp(jet.CAST(jet.String(v)).AS("inet"))
	}))
	key.only = dialects(dialectPostgres)
	return key
}

// checkInet accepts an address without a zone, or a network.
func checkInet(v string) error {
	if addr, err := netip.ParseAddr(v); err == nil {
		if addr.Zone() != "" {
			return errors.New("inet holds no IPv6 zone")
		}
		return nil
	}
	if _, err := netip.ParsePrefix(v); err == nil {
		return nil
	}
	return errors.New("not an inet value")
}

// UUIDKey orders on a NOT NULL uuid column. A cursor value that does not
// parse as a UUID is refused before the database sees it.
//
// On PostgreSQL the column is a uuid and the literal is cast to uuid. On MySQL
// (CHAR(36)) and SQLite (TEXT) the column holds the lower-case canonical text
// form, which sorts as the uuid does, and the literal is that text.
func UUIDKey[T any](column jet.ColumnString, value func(T) uuid.UUID) Key[T] {
	return uuidKey(column, false, ptrOf(value))
}

// NullableUUIDKey orders on a nullable uuid column.
func NullableUUIDKey[T any](column jet.ColumnString, value func(T) *uuid.UUID) Key[T] {
	return uuidKey(column, true, value)
}

func uuidKey[T any](column jet.ColumnString, nullable bool, value func(T) *uuid.UUID) Key[T] {
	codec := keyCodec[uuid.UUID]{
		encode: func(v uuid.UUID) any { return v.String() },
		decode: func(raw json.RawMessage) (uuid.UUID, error) {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return uuid.Nil, err
			}
			return uuid.Parse(text)
		},
	}
	return newKey(column, nullable, value, codec, literalBound(column, func(d dialect, v uuid.UUID) ijet.Expression {
		if d == dialectPostgres {
			return jet.UUID(v)
		}
		return ijet.String(v.String())
	}))
}
