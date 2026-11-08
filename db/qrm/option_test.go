// This file is Nanostack-only (not in go-jet). See db/UPSTREAM.md.

package qrm

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nanostack-dev/nanostack-framework/db/internal/optional"
	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// newTestScanContext builds a ScanContext the way NewScanContext does, from column
// aliases instead of *sql.Rows.
func newTestScanContext(aliases ...string) *ScanContext {
	commonIdentToColumnIndex := map[string]int{}
	for i, alias := range aliases {
		names := strings.SplitN(alias, ".", 2)
		ident := toCommonIdentifier(names[0])
		if len(names) > 1 {
			ident = concat(ident, ".", toCommonIdentifier(names[1]))
		}
		commonIdentToColumnIndex[ident] = i
	}
	return &ScanContext{
		row:                      createScanSlice(len(aliases)),
		uniqueDestObjectsMap:     map[string]int{},
		groupKeyInfoCache:        map[string]groupKeyInfo{},
		commonIdentToColumnIndex: commonIdentToColumnIndex,
		typeInfoMap:              map[string]typeInfo{},
		typesVisited:             newTypeStack(),
		columnAlias:              aliases,
		columnIndexRead:          make([]bool, len(aliases)),
	}
}

// scanRows maps rows (driver values, nil for NULL) into slicePtr like queryToSlice does.
func scanRows(t *testing.T, sc *ScanContext, slicePtr any, rows ...[]any) {
	t.Helper()
	slicePtrValue := reflect.ValueOf(slicePtr)
	for _, row := range rows {
		require.Len(t, row, len(sc.row))
		for i, v := range row {
			setCell(t, sc, i, v)
		}
		sc.rowNum++
		_, err := mapRowToSlice(sc, "", slicePtrValue, nil)
		require.NoError(t, err)
		if sc.rowNum == 1 {
			sc.ensureStrictness()
		}
	}
}

// setCell stores v as the driver value of column i of the current row.
func setCell(t *testing.T, sc *ScanContext, i int, v any) {
	t.Helper()
	cell, ok := sc.row[i].(*any)
	require.True(t, ok, "row cell %d is %T, not *any", i, sc.row[i])
	*cell = v
}

// scanOne maps one row into a fresh *T with mapRowToStruct.
func scanOne[T any](t *testing.T, aliases []string, row ...any) T {
	t.Helper()
	sc := newTestScanContext(aliases...)
	for i, v := range row {
		setCell(t, sc, i, v)
	}
	sc.rowNum = 1
	var dest T
	_, err := mapRowToStruct(sc, "", reflect.ValueOf(&dest), nil)
	require.NoError(t, err)
	return dest
}

type scalars struct {
	I32      functional.Option[int32]
	Time     functional.Option[time.Time]
	TimeTZ   functional.Option[time.Time]
	UUID     functional.Option[uuid.UUID]
	U64      functional.Option[uint64]
	U64Bytes functional.Option[uint64]
	Bool     functional.Option[bool]
	Bytes    functional.Option[[]byte]
	Str      functional.Option[string]
}

var scalarAliases = []string{
	"scalars.i32", "scalars.time", "scalars.time_tz", "scalars.uuid", "scalars.u64", "scalars.u64_bytes",
	"scalars.bool", "scalars.bytes", "scalars.str",
}

func TestOptionScalarsNull(t *testing.T) {
	got := scanOne[scalars](t, scalarAliases, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.Equal(t, scalars{}, got)
}

func TestOptionScalarsConvertLikePointers(t *testing.T) {
	id := uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	driverBytes := []byte("abc")

	got := scanOne[scalars](t, scalarAliases,
		int64(7),                       // int64 into Option[int32]
		[]byte("2024-01-02 03:04:05"),  // go-sql-driver/mysql DATETIME without parseTime
		"15:04:05-07",                  // pgx timetz
		id.String(),                    // pgx uuid: through uuid.UUID.Scan
		uint64(18446744073709551615),   // mysql binary protocol
		[]byte("18446744073709551615"), // mysql text protocol
		int64(1),                       // TINYINT(1)
		driverBytes,                    // bytea / blob
		[]byte("text"),                 // text as bytes
	)

	require.Equal(t, functional.Some[int32](7), got.I32)
	require.True(t, got.Time.Value().Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)), got.Time)
	wantTZ, err := time.Parse("15:04:05-07", "15:04:05-07")
	require.NoError(t, err)
	require.True(t, got.TimeTZ.Value().Equal(wantTZ), got.TimeTZ)
	require.Equal(t, functional.Some(id), got.UUID)
	require.Equal(t, functional.Some[uint64](18446744073709551615), got.U64)
	require.Equal(t, functional.Some[uint64](18446744073709551615), got.U64Bytes)
	require.Equal(t, functional.Some(true), got.Bool)
	require.Equal(t, functional.Some("text"), got.Str)

	driverBytes[0] = 'X'
	require.Equal(t, functional.Some([]byte("abc")), got.Bytes, "the driver's buffer must be copied")
}

func TestOptionPresentZeroIsSome(t *testing.T) {
	got := scanOne[scalars](t, scalarAliases, int64(0), nil, nil, nil, uint64(0), nil, int64(0), []byte{}, "")
	require.Equal(t, functional.Some[int32](0), got.I32)
	require.Equal(t, functional.Some[uint64](0), got.U64)
	require.Equal(t, functional.Some(false), got.Bool)
	require.Equal(t, functional.Some(""), got.Str)
	require.True(t, got.Bytes.IsPresent())
}

func TestOptionAssignError(t *testing.T) {
	sc := newTestScanContext("scalars.i32")
	setCell(t, sc, 0, "not a number")
	sc.rowNum = 1
	var dest struct {
		I32 functional.Option[int32] `alias:"scalars.i32"`
	}
	_, err := mapRowToStruct(sc, "", reflect.ValueOf(&dest), nil)
	require.ErrorContains(t, err, `can't assign string("not a number") to 'I32 functional.Option[int32]'`)
}

type jsonPayload struct {
	A int    `json:"a"`
	B string `json:"b"`
}

func TestOptionJsonColumn(t *testing.T) {
	type dest struct {
		Payload functional.Option[jsonPayload] `json_column:"payload"`
	}
	got := scanOne[dest](t, []string{"payload"}, []byte(`{"a":1,"b":"x"}`))
	require.Equal(t, functional.Some(jsonPayload{A: 1, B: "x"}), got.Payload)

	got = scanOne[dest](t, []string{"payload"}, nil)
	require.False(t, got.Payload.IsPresent())
}

// Nested destinations, modelled on dvds: a Film with an optional original language,
// and an actor with films.

type Language struct {
	LanguageID int64 `sql:"primary_key"`
	Name       functional.Option[string]
}

type FilmCategory struct {
	CategoryID int64 `sql:"primary_key"`
}

type LanguageWithCategories struct {
	Language
	Categories []FilmCategory
}

type Film struct {
	FilmID int64 `sql:"primary_key"`
	Title  string
}

type FilmWithLanguage struct {
	Film
	OriginalLanguage functional.Option[Language] `alias:"original_language.*"`
}

func TestOptionNestedStruct(t *testing.T) {
	sc := newTestScanContext("film.film_id", "film.title", "original_language.language_id", "original_language.name")
	var dest []FilmWithLanguage
	scanRows(t, sc, &dest,
		[]any{int64(1), "A", int64(2), "Italian"},
		[]any{int64(2), "B", nil, nil},
		[]any{int64(3), "C", int64(3), nil}, // a NULL column inside a present struct
	)

	require.Equal(t, []FilmWithLanguage{
		{
			FilmID: 1, Title: "A",
			OriginalLanguage: functional.Some(Language{LanguageID: 2, Name: functional.Some("Italian")}),
		},
		{FilmID: 2, Title: "B"},
		{FilmID: 3, Title: "C", OriginalLanguage: functional.Some(Language{LanguageID: 3})},
	}, dest)
}

func TestOptionNestedStructKeepsGrowingAcrossRows(t *testing.T) {
	type dest struct {
		Film
		Language functional.Option[LanguageWithCategories]
	}
	sc := newTestScanContext(
		"film.film_id",
		"film.title",
		"language.language_id",
		"language.name",
		"film_category.category_id",
	)
	var got []dest
	scanRows(t, sc, &got,
		[]any{int64(1), "A", int64(2), "Italian", int64(10)},
		[]any{int64(1), "A", int64(2), "Italian", int64(11)},
		[]any{int64(2), "B", nil, nil, nil},
	)

	require.Len(t, got, 2)
	lang, ok := got[0].Language.Get()
	require.True(t, ok)
	require.Equal(t, []FilmCategory{{CategoryID: 10}, {CategoryID: 11}}, lang.Categories)
	require.False(t, got[1].Language.IsPresent())
}

func TestOptionNestedStructGroupKey(t *testing.T) {
	// Two rows of the same actor, joined to two films that both have the same optional
	// language: the language's primary key takes part in grouping through the Option.
	type actor struct {
		ActorID int64 `sql:"primary_key"`
		Films   []FilmWithLanguage
	}
	sc := newTestScanContext(
		"actor.actor_id",
		"film.film_id",
		"film.title",
		"original_language.language_id",
		"original_language.name",
	)
	var got []actor
	scanRows(t, sc, &got,
		[]any{int64(1), int64(10), "A", int64(2), "Italian"},
		[]any{int64(1), int64(11), "B", nil, nil},
		[]any{int64(1), int64(10), "A", int64(2), "Italian"}, // duplicate row: grouped away
	)

	require.Len(t, got, 1)
	require.Len(t, got[0].Films, 2)
	require.Equal(
		t,
		functional.Some(Language{LanguageID: 2, Name: functional.Some("Italian")}),
		got[0].Films[0].OriginalLanguage,
	)
	require.False(t, got[0].Films[1].OriginalLanguage.IsPresent())

	info := sc.getGroupKeyInfo(reflect.TypeFor[FilmWithLanguage](), nil, new(newTypeStack()))
	// The Option[Language] field contributes original_language.language_id (column 3,
	// found through its alias tag) to the group key, like a *Language field would.
	require.Equal(t, []groupKeyInfo{
		{typeName: "Film", pkIndexes: []int{1}},
		{typeName: "Language", pkIndexes: []int{3}},
	}, info.subTypes)
}

func TestOptionUnsupportedShapesPanic(t *testing.T) {
	cases := []struct {
		name string
		run  func()
		msg  string
	}{
		{"pointer element", func() {
			scanOne[struct{ X functional.Option[*Language] }](t, []string{"x"}, int64(1))
		}, "jet: functional.Option of a pointer is not supported (field X functional.Option[*github.com/nanostack-dev/nanostack-framework/db/qrm.Language]); use functional.Option[qrm.Language] or *qrm.Language"},
		{"pointer to option", func() {
			scanOne[struct{ X *functional.Option[int64] }](t, []string{"x"}, int64(1))
		}, "jet: a pointer to functional.Option is not supported (field X *functional.Option[int64]); use functional.Option[int64]"},
		{"nested option", func() {
			scanOne[struct {
				X functional.Option[functional.Option[int]]
			}](t, []string{"x"}, int64(1))
		}, "jet: nested functional.Option is not supported (field X functional.Option[github.com/nanostack-dev/nanostack-framework/pkg/functional.Option[int]])"},
		{"slice of structs", func() {
			scanOne[struct{ X functional.Option[[]Language] }](t, []string{"x"}, int64(1))
		}, "jet: unsupported functional.Option element []qrm.Language (field X functional.Option[[]github.com/nanostack-dev/nanostack-framework/db/qrm.Language]); use []qrm.Language, or a json_column tag for JSON"},
		{"slice of options", func() {
			sc := newTestScanContext("x")
			var dest []functional.Option[int]
			scanRows(t, sc, &dest, []any{int64(1)})
		}, "jet: functional.Option[T] is not a supported row destination ([]functional.Option[T] or a top-level Option); scan into []T, where NULL rows are skipped, or use the db/query helpers, which return functional.Option"},
		{"nested slice of options", func() {
			type dest struct {
				ID int64 `sql:"primary_key"`
				X  []functional.Option[int]
			}
			sc := newTestScanContext("dest.id", "x")
			var got []dest
			scanRows(t, sc, &got, []any{int64(1), int64(1)})
		}, "jet: functional.Option[T] is not a supported row destination at 'X []functional.Option[int]' ([]functional.Option[T] or a top-level Option); scan into []T, where NULL rows are skipped, or use the db/query helpers, which return functional.Option"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.PanicsWithValue(t, c.msg, c.run)
		})
	}
}

func TestOptionStrictScan(t *testing.T) {
	defer func(c Config) { GlobalConfig = c }(GlobalConfig)
	GlobalConfig.StrictScan = true

	sc := newTestScanContext(scalarAliases...)
	var dest []scalars
	require.NotPanics(t, func() {
		scanRows(t, sc, &dest, []any{int64(1), nil, nil, nil, nil, nil, nil, nil, nil})
	})
}

func TestOptionStrictFieldMapping(t *testing.T) {
	defer func(c Config) { GlobalConfig = c }(GlobalConfig)
	GlobalConfig.StrictFieldMapping = true

	type dest struct {
		Film
		Missing          functional.Option[string]
		OriginalLanguage functional.Option[Language] `alias:"original_language.*"`
	}
	sc := newTestScanContext("film.film_id", "film.title")
	var got []dest
	require.PanicsWithValue(
		t,
		"jet: fields never mapped: 'dest.Missing', 'OriginalLanguage Language.LanguageID', 'OriginalLanguage Language.Name'",
		func() { scanRows(t, sc, &got, []any{int64(1), "A"}) },
	)
}

// TestOptionScannerIsClassifiedOnce pins that the Scanner check runs when the field is
// classified, not for every scanned value: an Option of a Scanner and an Option of a
// simple type get different mapping kinds.
func TestOptionScannerIsClassifiedOnce(t *testing.T) {
	type dest struct {
		ID    functional.Option[uuid.UUID]
		Count functional.Option[int64]
	}
	typ := reflect.TypeFor[dest]()
	for name, want := range map[string]fieldMappingType{"ID": optionalScannerType, "Count": optionalType} {
		field, _ := typ.FieldByName(name)
		elem, ok := optional.Elem(field.Type)
		require.True(t, ok)
		require.Equal(t, want, classifyOption(elem, field), name)
	}
}
