package keyset

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/google/uuid"

	jet "github.com/nanostack-dev/nanostack-framework/db/postgres"
)

// StringKey orders on a NOT NULL text column.
func StringKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return stringKey(column, false, ptrOf(value))
}

// NullableStringKey orders on a nullable text column.
func NullableStringKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return stringKey(column, true, value)
}

// textCodec refuses a NUL byte, which PostgreSQL text cannot hold: binding it
// would fail in the database and not as an invalid cursor.
func textCodec() keyCodec[string] {
	return keyCodec[string]{
		encode: func(v string) any { return v },
		decode: func(raw json.RawMessage) (string, error) {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return "", err
			}
			if strings.ContainsRune(v, 0) {
				return "", errors.New("text contains NUL")
			}
			return v, nil
		},
	}
}

func stringKey[T any](column jet.ColumnString, nullable bool, value func(T) *string) Key[T] {
	return newCodecKey(column, nullable, value, textCodec(),
		func(v string) jet.Expression { return jet.String(v) },
		func(v string) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.String(v)), column.LT(jet.String(v)), column.EQ(jet.String(v))
		})
}

// EnumKey orders on a NOT NULL PostgreSQL enum column. PostgreSQL orders an
// enum by declaration order and refuses to compare it with text, so the
// cursor value is cast to enumType, the enum's SQL type name. labels lists the
// enum's values: a cursor that carries any other value is refused before the
// cast could fail in the database.
//
// enumType is written into the SQL as raw text, so it must match
// ^[a-z_][a-z0-9_.]*$ (for example "order_status" or "public.order_status").
// EnumKey panics otherwise.
//
// A text column that holds enum-like values is a StringKey: it sorts as text.
func EnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) string) Key[T] {
	return enumKey(column, enumType, labels, false, ptrOf(value))
}

// NullableEnumKey orders on a nullable PostgreSQL enum column. enumType is
// checked as in EnumKey.
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
	typed := func(v string) jet.StringExpression {
		return jet.StringExp(jet.CAST(jet.String(v)).AS(enumType))
	}
	allowed := slices.Clone(labels)
	codec := keyCodec[string]{
		encode: func(v string) any { return v },
		decode: func(raw json.RawMessage) (string, error) {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return "", err
			}
			if !slices.Contains(allowed, v) {
				return "", fmt.Errorf("not a %s label", enumType)
			}
			return v, nil
		},
	}
	return newCodecKey(column, nullable, value, codec,
		func(v string) jet.Expression { return typed(v) },
		func(v string) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
}

// InetKey orders on a NOT NULL inet column, read as its text form. PostgreSQL
// refuses to compare inet with text, so the cursor value is cast to inet. A
// value that is not an address or a network, or an address with an IPv6 zone
// (fe80::1%eth0), which inet cannot hold, is refused before the cast.
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
			return v, checkInet(v)
		},
	}
	typed := func(v string) jet.StringExpression { return jet.StringExp(jet.CAST(jet.String(v)).AS("inet")) }
	return newCodecKey(column, nullable, value, codec,
		func(v string) jet.Expression { return typed(v) },
		func(v string) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
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
// parse as a UUID is refused before the uuid cast could fail in the database.
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
	return newCodecKey(column, nullable, value, codec,
		func(v uuid.UUID) jet.Expression { return jet.UUID(v) },
		func(v uuid.UUID) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.UUID(v)), column.LT(jet.UUID(v)), column.EQ(jet.UUID(v))
		})
}
