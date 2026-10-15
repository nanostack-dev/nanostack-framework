package transactor

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/pkg/jetx"
	"github.com/nanostack-dev/nanostack-framework/pkg/search"
)

// Key is one column of a keyset ordering over scanned rows of T: the column
// to order and compare on, and how to read that column's value from a row.
//
// Build keys with the typed constructors (StringKey, UUIDKey, EnumKey,
// TimeKey, LocalTimeKey, IntKey, FloatKey and their Nullable variants) so the
// cursor value and the SQL literal share one type. A constructor rejects a
// cursor value its column cannot hold with search.ErrInvalidCursor, before
// any query runs.
type Key[T any] struct {
	column   jet.Column
	nullable bool
	// value reads the row's value, nil for SQL NULL.
	value func(T) any
	// bound decodes a cursor value into the comparisons against it. A JSON
	// null decodes to a bound with isNull set.
	bound func(json.RawMessage) (keyBound, error)
}

// keyBound holds the comparisons of one column against one cursor value.
type keyBound struct {
	isNull  bool
	literal jet.Expression
	gt      jet.BoolExpression
	lt      jet.BoolExpression
	eq      jet.BoolExpression
}

// keyCodec converts a column value to and from its cursor form. encode runs
// when a cursor is issued; decode when one comes back, and must reject any
// value the column cannot hold so a forged cursor fails as ErrInvalidCursor
// rather than as a database error.
type keyCodec[V any] struct {
	encode func(V) any
	decode func(json.RawMessage) (V, error)
}

func jsonCodec[V any]() keyCodec[V] {
	return keyCodec[V]{
		encode: func(v V) any { return v },
		decode: func(raw json.RawMessage) (V, error) {
			var v V
			err := json.Unmarshal(raw, &v)
			return v, err
		},
	}
}

func newKey[T any, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	literal func(V) jet.Expression,
	compare func(V) (gt, lt, eq jet.BoolExpression),
) Key[T] {
	return newCodecKey(column, nullable, value, jsonCodec[V](), literal, compare)
}

func newCodecKey[T any, V any](
	column jet.Column,
	nullable bool,
	value func(T) *V,
	codec keyCodec[V],
	literal func(V) jet.Expression,
	compare func(V) (gt, lt, eq jet.BoolExpression),
) Key[T] {
	return Key[T]{
		column:   column,
		nullable: nullable,
		value: func(row T) any {
			if v := value(row); v != nil {
				return codec.encode(*v)
			}
			return nil
		},
		bound: func(raw json.RawMessage) (keyBound, error) {
			if string(raw) == "null" {
				if !nullable {
					return keyBound{}, fmt.Errorf("%w: null for non-null column %s", search.ErrInvalidCursor, column.Name())
				}
				return keyBound{isNull: true, eq: column.IS_NULL()}, nil
			}
			v, err := codec.decode(raw)
			if err != nil {
				return keyBound{}, fmt.Errorf("%w: value for column %s", search.ErrInvalidCursor, column.Name())
			}
			gt, lt, eq := compare(v)
			return keyBound{literal: literal(v), gt: gt, lt: lt, eq: eq}, nil
		},
	}
}

func ptrOf[T any, V any](value func(T) V) func(T) *V {
	return func(row T) *V {
		v := value(row)
		return &v
	}
}

// StringKey orders on a NOT NULL text column.
func StringKey[T any](column jet.ColumnString, value func(T) string) Key[T] {
	return stringKey(column, false, ptrOf(value))
}

// NullableStringKey orders on a nullable text column.
func NullableStringKey[T any](column jet.ColumnString, value func(T) *string) Key[T] {
	return stringKey(column, true, value)
}

// textCodec rejects a NUL byte, which Postgres text cannot hold: binding it
// would fail in the database rather than as an invalid cursor.
func textCodec() keyCodec[string] {
	return keyCodec[string]{
		encode: func(v string) any { return v },
		decode: func(raw json.RawMessage) (string, error) {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return "", err
			}
			if strings.ContainsRune(v, 0) {
				return "", fmt.Errorf("text contains NUL")
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

// EnumKey orders on a NOT NULL Postgres enum column. Postgres orders an enum
// by declaration order and refuses to compare it with text, so the cursor value
// is cast to enumType, the enum's SQL type name. labels lists the enum's
// values: a cursor carrying any other value is rejected before the cast could
// fail in the database.
//
// A text column holding enum-like values is a StringKey: it sorts as text.
func EnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) string) Key[T] {
	return enumKey(column, enumType, labels, false, ptrOf(value))
}

// NullableEnumKey orders on a nullable Postgres enum column.
func NullableEnumKey[T any](column jet.ColumnString, enumType string, labels []string, value func(T) *string) Key[T] {
	return enumKey(column, enumType, labels, true, value)
}

func enumKey[T any](column jet.ColumnString, enumType string, labels []string, nullable bool, value func(T) *string) Key[T] {
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

// UUIDKey orders on a NOT NULL uuid column. A cursor value that does not
// parse as a UUID is rejected before the uuid cast could fail in the database.
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

// TimeKey orders on a NOT NULL timestamptz column.
func TimeKey[T any](column jet.ColumnTimestampz, value func(T) time.Time) Key[T] {
	return timeKey(column, false, ptrOf(value))
}

// NullableTimeKey orders on a nullable timestamptz column.
func NullableTimeKey[T any](column jet.ColumnTimestampz, value func(T) *time.Time) Key[T] {
	return timeKey(column, true, value)
}

// Postgres stores timestamps from 4714-11-24 BC to 294276-12-31 AD at
// microsecond precision.
var (
	postgresMinTime = time.Date(-4713, time.November, 24, 0, 0, 0, 0, time.UTC)
	postgresMaxTime = time.Date(294276, time.December, 31, 23, 59, 59, 999_999_000, time.UTC)
	// postgresEpochUnix is 2000-01-01 UTC, the Postgres timestamp epoch.
	postgresEpochUnix = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
)

// microsCodec encodes an instant as microseconds since 2000-01-01 UTC, the
// representation Postgres itself stores. It is exact across the whole
// Postgres range, which Unix microseconds overflow past year 294247, where
// RFC 3339 loses seconds offsets and cannot write years past 9999, and a
// decoded value carries no zone the database could refuse. The arithmetic
// goes through seconds, since a time.Duration overflows after 292 years.
func microsCodec() keyCodec[time.Time] {
	return keyCodec[time.Time]{
		encode: func(v time.Time) any {
			return (v.Unix()-postgresEpochUnix)*1_000_000 + int64(v.Nanosecond()/1_000)
		},
		decode: func(raw json.RawMessage) (time.Time, error) {
			var micros int64
			if err := json.Unmarshal(raw, &micros); err != nil {
				return time.Time{}, err
			}
			seconds, remainder := micros/1_000_000, micros%1_000_000
			if remainder < 0 {
				seconds, remainder = seconds-1, remainder+1_000_000
			}
			decoded := time.Unix(postgresEpochUnix+seconds, remainder*1_000).UTC()
			if decoded.Before(postgresMinTime) || decoded.After(postgresMaxTime) {
				return time.Time{}, fmt.Errorf("timestamp outside the Postgres range")
			}
			return decoded, nil
		},
	}
}

func timeKey[T any](column jet.ColumnTimestampz, nullable bool, value func(T) *time.Time) Key[T] {
	return newCodecKey(column, nullable, value, microsCodec(),
		func(v time.Time) jet.Expression { return jet.TimestampzT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampzT(v)), column.LT(jet.TimestampzT(v)), column.EQ(jet.TimestampzT(v))
		})
}

// LocalTimeKey orders on a NOT NULL timestamp (without time zone) column.
func LocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) time.Time) Key[T] {
	return localTimeKey(column, false, ptrOf(value))
}

// NullableLocalTimeKey orders on a nullable timestamp (without time zone) column.
func NullableLocalTimeKey[T any](column jet.ColumnTimestamp, value func(T) *time.Time) Key[T] {
	return localTimeKey(column, true, value)
}

func localTimeKey[T any](column jet.ColumnTimestamp, nullable bool, value func(T) *time.Time) Key[T] {
	// A timestamp has no zone; its wall clock is the value. Read the wall
	// clock as if it were UTC, so the microseconds round trip cannot shift it.
	micros := microsCodec()
	codec := keyCodec[time.Time]{
		encode: func(v time.Time) any {
			return micros.encode(time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC))
		},
		decode: micros.decode,
	}
	return newCodecKey(column, nullable, value, codec,
		func(v time.Time) jet.Expression { return jet.TimestampT(v) },
		func(v time.Time) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(jet.TimestampT(v)), column.LT(jet.TimestampT(v)), column.EQ(jet.TimestampT(v))
		})
}

// IntKey orders on a NOT NULL integer column.
func IntKey[T any](column jet.ColumnInteger, value func(T) int64) Key[T] {
	return intKey(column, false, ptrOf(value))
}

// NullableIntKey orders on a nullable integer column.
func NullableIntKey[T any](column jet.ColumnInteger, value func(T) *int64) Key[T] {
	return intKey(column, true, value)
}

func intKey[T any](column jet.ColumnInteger, nullable bool, value func(T) *int64) Key[T] {
	// The literal is cast to bigint: a bare parameter would take the column's
	// type, and a forged value past smallint or integer would fail to bind.
	// Postgres compares integer widths across one btree family, so an index
	// on the column still serves the comparison.
	typed := func(v int64) jet.IntegerExpression { return jet.IntExp(jet.CAST(jet.Int(v)).AS_BIGINT()) }
	return newKey(column, nullable, value,
		func(v int64) jet.Expression { return typed(v) },
		func(v int64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
}

// FloatKey orders on a NOT NULL double precision column. It does not fit
// numeric, where float64 rounding would repeat a row forever, nor real, which
// takes RealKey. NaN and the infinities, which Postgres orders above every
// number, round-trip exactly.
func FloatKey[T any](column jet.ColumnFloat, value func(T) float64) Key[T] {
	return floatKey(column, false, ptrOf(value))
}

// NullableFloatKey orders on a nullable double precision column.
func NullableFloatKey[T any](column jet.ColumnFloat, value func(T) *float64) Key[T] {
	return floatKey(column, true, value)
}

// typedFloat casts the literal to double precision explicitly.
func typedFloat(v float64) jet.FloatExpression {
	return jet.FloatExp(jet.CAST(jet.Float(v)).AS_DOUBLE())
}

// RealKey orders on a NOT NULL real column. A real compared with a double
// literal is promoted, and 0.1 stored as real is not 0.1 as double, so the
// boundary row would match itself and repeat. The key compares as real, and
// rejects a cursor value outside real's range before the cast could fail.
func RealKey[T any](column jet.ColumnFloat, value func(T) float32) Key[T] {
	return realKey(column, false, func(row T) *float32 {
		v := value(row)
		return &v
	})
}

// NullableRealKey orders on a nullable real column.
func NullableRealKey[T any](column jet.ColumnFloat, value func(T) *float32) Key[T] {
	return realKey(column, true, value)
}

func realKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float32) Key[T] {
	double := floatCodec()
	codec := keyCodec[float32]{
		encode: func(v float32) any { return double.encode(float64(v)) },
		decode: func(raw json.RawMessage) (float32, error) {
			v, err := double.decode(raw)
			if err != nil {
				return 0, err
			}
			if !math.IsInf(v, 0) && !math.IsNaN(v) && math.Abs(v) > math.MaxFloat32 {
				return 0, fmt.Errorf("outside the real range")
			}
			return float32(v), nil
		},
	}
	typed := func(v float32) jet.FloatExpression {
		return jet.FloatExp(jet.CAST(jet.Float(float64(v))).AS_REAL())
	}
	return newCodecKey(column, nullable, value, codec,
		func(v float32) jet.Expression { return typed(v) },
		func(v float32) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typed(v)), column.LT(typed(v)), column.EQ(typed(v))
		})
}

// floatCodec writes NaN and the infinities as words, which JSON numbers
// cannot express.
func floatCodec() keyCodec[float64] {
	return keyCodec[float64]{
		encode: func(v float64) any {
			switch {
			case math.IsNaN(v):
				return "NaN"
			case math.IsInf(v, 1):
				return "Infinity"
			case math.IsInf(v, -1):
				return "-Infinity"
			default:
				return v
			}
		},
		decode: func(raw json.RawMessage) (float64, error) {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				switch text {
				case "NaN":
					return math.NaN(), nil
				case "Infinity":
					return math.Inf(1), nil
				case "-Infinity":
					return math.Inf(-1), nil
				default:
					return 0, fmt.Errorf("unknown float %q", text)
				}
			}
			var v float64
			err := json.Unmarshal(raw, &v)
			return v, err
		},
	}
}

func floatKey[T any](column jet.ColumnFloat, nullable bool, value func(T) *float64) Key[T] {
	codec := floatCodec()
	return newCodecKey(column, nullable, value, codec,
		func(v float64) jet.Expression { return typedFloat(v) },
		func(v float64) (jet.BoolExpression, jet.BoolExpression, jet.BoolExpression) {
			return column.GT(typedFloat(v)), column.LT(typedFloat(v)), column.EQ(typedFloat(v))
		})
}

type orderedKey[T any] struct {
	key       Key[T]
	direction search.SortDirection
}

// Order is a total keyset ordering over rows of T: the requested sort columns
// followed by a unique tiebreak column. Because the tiebreak is unique, every
// row has exactly one position, so a cursor (the last row's values) splits the
// result into the rows before it and the rows after it with no skips or
// repeats, however rows are inserted or deleted between pages.
//
// Build one with Keys and hand it to PageBuilder.Keyset. A repository whose
// query does not fit PageBuilder uses OrderBy, Seek and Trim directly.
type Order[T any] struct {
	keys      []orderedKey[T]
	signature string
}

// Keys builds the keyset ordering for a search's sort fields.
//
// columns maps each sortable field to its key; fields absent from the map are
// ignored, as with SortColumns. The tiebreak keys must together be unique and
// NOT NULL within the rows the search can return, normally the primary key.
// A table with a composite key passes every column the search scope does not
// already fix, for example the member id for a search scoped to one
// organization, or both columns otherwise. The tiebreak sorts in the direction
// of the last requested field (ascending when none), so a composite index on
// (sort, id) serves the query in one direction scan. A column requested twice
// keeps its first position, and a tiebreak column is not repeated when it was
// requested.
func Keys[S ~string, T any](sorts []search.Sort[S], columns map[S]Key[T], tiebreak Key[T], more ...Key[T]) Order[T] {
	order := Order[T]{}
	seen := map[string]bool{}
	direction := search.SortAscending
	for _, sort := range sorts {
		key, ok := columns[sort.Field]
		if !ok {
			continue
		}
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		direction = normalizeDirection(sort.Direction)
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	for _, key := range append([]Key[T]{tiebreak}, more...) {
		name := qualifiedName(key.column)
		if seen[name] {
			continue
		}
		seen[name] = true
		order.keys = append(order.keys, orderedKey[T]{key: key, direction: direction})
	}
	parts := make([]string, len(order.keys))
	for i, k := range order.keys {
		parts[i] = qualifiedName(k.key.column) + ":" + string(k.direction)
	}
	order.signature = strings.Join(parts, ",")
	return order
}

func normalizeDirection(direction search.SortDirection) search.SortDirection {
	if direction == search.SortDescending {
		return search.SortDescending
	}
	return search.SortAscending
}

// qualifiedName identifies a column by the SQL that references it, so two
// keys collide only when they are the same column. A column projected from a
// subquery renders as "alias.table.column" and stays distinct from its base
// table column.
func qualifiedName(column jet.Column) string {
	const marker = " IS NULL"
	rendered := jet.SELECT(jet.Int(1)).WHERE(column.IS_NULL()).DebugSql()
	if start := strings.Index(rendered, "WHERE "); start >= 0 {
		rest := rendered[start+len("WHERE "):]
		if end := strings.Index(rest, marker); end >= 0 {
			return strings.ReplaceAll(strings.Trim(rest[:end], "()"), `"`, "")
		}
	}
	if table := column.TableName(); table != "" {
		return table + "." + column.Name()
	}
	return column.Name()
}

// Signature identifies the ordering, for example
// "widgets.created_at:DESC,widgets.id:DESC". Cursors embed it.
func (o Order[T]) Signature() string {
	return o.signature
}

// OrderBy returns the ORDER BY clauses, tiebreak included. Postgres defaults
// apply to NULLs: last when ascending, first when descending. Seek relies on
// exactly that placement.
func (o Order[T]) OrderBy() []jet.OrderByClause {
	clauses := make([]jet.OrderByClause, len(o.keys))
	for i, k := range o.keys {
		clauses[i] = jetx.OrderBy(k.key.column, k.direction)
	}
	return clauses
}

// Seek returns the predicate selecting the rows strictly after cursor in this
// ordering, or nil for an empty cursor. A cursor issued for another ordering,
// or one that does not decode, fails with search.ErrInvalidCursor.
func (o Order[T]) Seek(cursor string) (jet.BoolExpression, error) {
	if cursor == "" {
		return nil, nil
	}
	decoded, err := search.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	if decoded.Order != o.signature {
		return nil, fmt.Errorf("%w: issued for a different sort", search.ErrInvalidCursor)
	}
	if len(decoded.Values) != len(o.keys) {
		return nil, fmt.Errorf("%w: wrong number of values", search.ErrInvalidCursor)
	}
	bounds := make([]keyBound, len(o.keys))
	for i, k := range o.keys {
		if bounds[i], err = k.key.bound(decoded.Values[i]); err != nil {
			return nil, err
		}
	}
	if row := o.rowSeek(bounds); row != nil {
		return row, nil
	}
	return o.expandedSeek(bounds), nil
}

// rowSeek uses a row-value comparison, which Postgres can answer with one
// range scan of a matching composite index. It only applies when every key is
// NOT NULL and all keys share a direction; otherwise it returns nil.
func (o Order[T]) rowSeek(bounds []keyBound) jet.BoolExpression {
	direction := o.keys[0].direction
	columns := make([]jet.Expression, len(o.keys))
	literals := make([]jet.Expression, len(o.keys))
	for i, k := range o.keys {
		if k.key.nullable || k.direction != direction {
			return nil
		}
		columns[i] = k.key.column
		literals[i] = bounds[i].literal
	}
	if direction == search.SortDescending {
		return jet.ROW(columns...).LT(jet.ROW(literals...))
	}
	return jet.ROW(columns...).GT(jet.ROW(literals...))
}

// expandedSeek spells the comparison out key by key:
//
//	(k1 after v1) OR (k1 = v1 AND k2 after v2) OR ... (... AND id after vid)
//
// "after" follows the ORDER BY placement of NULLs: ascending sorts them last,
// so every non-null value has NULLs after it and nothing follows a NULL;
// descending sorts them first, so a NULL is followed by every non-null value.
func (o Order[T]) expandedSeek(bounds []keyBound) jet.BoolExpression {
	var disjuncts []jet.BoolExpression
	var prefix []jet.BoolExpression
	for i, k := range o.keys {
		if after := afterBound(k, bounds[i]); after != nil {
			disjuncts = append(disjuncts, jetx.CombineFilters(append(append([]jet.BoolExpression{}, prefix...), after)...))
		}
		prefix = append(prefix, bounds[i].eq)
	}
	if len(disjuncts) == 0 {
		// Only reachable when every value is a trailing ascending NULL, which a
		// unique NOT NULL tiebreak rules out; select nothing rather than all.
		return jet.Bool(false)
	}
	return jetx.CombineFiltersWithOr(disjuncts...)
}

func afterBound[T any](k orderedKey[T], bound keyBound) jet.BoolExpression {
	column := k.key.column
	if k.direction == search.SortDescending {
		if bound.isNull {
			return column.IS_NOT_NULL()
		}
		return bound.lt
	}
	if bound.isNull {
		return nil
	}
	if k.key.nullable {
		return bound.gt.OR(column.IS_NULL())
	}
	return bound.gt
}

// Cursor encodes the position of row, the last row of a page.
func (o Order[T]) Cursor(row T) (string, error) {
	values := make([]any, len(o.keys))
	for i, k := range o.keys {
		values[i] = k.key.value(row)
	}
	return search.EncodeCursor(o.signature, values)
}

// Trim cuts rows fetched with LIMIT limit+1 down to one page. When the extra
// row is present it returns the cursor of the page's last row; otherwise the
// page is the end and the cursor is empty.
func (o Order[T]) Trim(rows []T, limit int) ([]T, string, error) {
	if limit <= 0 || len(rows) <= limit {
		return rows, "", nil
	}
	page := rows[:limit]
	next, err := o.Cursor(page[limit-1])
	if err != nil {
		return nil, "", err
	}
	return page, next, nil
}
