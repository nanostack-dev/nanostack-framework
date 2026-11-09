package nanorepo

import "context"

// Column is satisfied by every column of a table type that nano gen db
// generates.
type Column interface {
	Name() string
}

// SearchOption configures one search declaration. Implement and Base take
// none. Make search options only with the functions of this package: Go cannot
// stop a type that embeds an option, a zero value or nil, so nano gen repo
// reads each argument of Search from source and refuses an argument that is
// not a direct call of an option function of this package (a variable, a
// spread slice, a call of another function or a composite literal).
type SearchOption interface {
	searchOption()
}

// option is the search option that the functions of this package return.
type option struct{}

func (option) searchOption() {}

// Search declares a search named name over table, which reads rows of type
// Row. Row is the generated model of table, or a struct that embeds the models
// of the tables it reads and tags other fields with alias:"table.column".
// nano gen repo generates the scope type, the New<name>Search constructor, the
// <name>Sort enum and the query builder. With API, it also generates the API
// mapper. A method of the repository interface that takes the query and
// returns a search result is generated on the implementation, unless the
// implementation has a hand-written method of that name.
//
// Like Implement, a search is a call inside func _() { ... }: Go type-checks
// it and never runs it, and nano gen repo reads it from source. Columns and
// tables are Go values of the generated table types, so a missing column is a
// compile error and a rename updates the declaration.
//
//	func _() {
//		nanorepo.Search[model.WebhookRequests]("WebhookRequest", table.WebhookRequests,
//			nanorepo.Map((*WebhookMapper).RequestToDomain),
//			nanorepo.From(requestsWithTheirWebhook),
//			nanorepo.Scope(table.Webhooks.OrganizationID),
//			nanorepo.Scope(table.WebhookRequests.WebhookID),
//			nanorepo.ScopeFunc(workspacescope.Filter, table.Webhooks.WorkspaceID),
//			nanorepo.Filter(table.WebhookRequests.Method, nanorepo.Eq, nanorepo.In),
//			nanorepo.Filter(table.WebhookRequests.ReceivedAt, nanorepo.Range("received_since", "received_until")),
//			nanorepo.FullText(table.WebhookRequests.Path),
//			nanorepo.Sort(table.WebhookRequests.ReceivedAt, table.WebhookRequests.Method),
//			nanorepo.DefaultSort(table.WebhookRequests.ReceivedAt, nanorepo.Desc),
//			nanorepo.API[apigen.WebhookRequestSearchRequest](),
//		)
//	}
//
// The functions that a declaration names (From, Condition, FilterExpr, Flag,
// FullTextExpr, SortExpr) are typed SQL of Nanostack DB. nano gen repo reads
// their bodies to find every table instance that the search reads, and it
// refuses raw SQL and calls that it cannot follow.
//
// The declarations compile with the tables of every dialect, because the
// conditions and expressions are type parameters. In this release, nano gen
// repo generates PostgreSQL searches only: a search on a MySQL or SQLite table
// is a gen error (E16, ADR 0027 decision 7).
//
// The search functions do nothing at run time. Their parameters document the
// call, and nano gen repo reads the arguments from source.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Search[Row any](name string, table Table, options ...SearchOption) {}

// Map maps each row of the search to the domain type with fn. fn is a
// func(Row) D, or a method expression whose receiver is the implementation or
// the type of one of its fields: nanorepo.Map((*WebhookMapper).RequestToDomain).
// Without Map or MapPage, the search returns the rows.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func Map[F any](fn F) SearchOption { return option{} }

// MapPage maps one page of rows with one call to fn, so that the mapper can
// load related rows in one statement. fn is a
// func(context.Context, []Row) ([]D, error), or a method expression with the
// same receivers as Map.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func MapPage[F any](fn F) SearchOption { return option{} }

// From makes the search read a join instead of the bare table. fn is a
// func() T, or a func(P) T where P is the generated scope type of the search,
// and T is a readable table of Nanostack DB. The second form can put a scope
// value in an ON clause:
//
//	func licensesOf(s OrganizationLicenseScope) postgres.ReadableTable {
//		return table.Organizations.LEFT_JOIN(table.OrganizationLicenses,
//			table.OrganizationLicenses.OrganizationID.EQ(table.Organizations.ID).
//				AND(table.OrganizationLicenses.PlatformTenantID.EQ(postgres.String(s.PlatformTenantID))))
//	}
//
// Each table of the join is an instance, and an alias of a table is its own
// instance. Every instance and every tenant column of every instance must be
// covered, for example by a scope, or by an ON clause that equates the column
// with a covered tenant column or a scope field. S6.2 of ADR 0027 gives the
// full rule, and Unscoped gives the only exemption of a hard tenant column.
// nano gen repo refuses CROSS, FULL and RIGHT joins.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func From[F any](fn F) SearchOption { return option{} }

// Scope makes the search require a value for column and more: the value is a
// field of the generated scope type, named from column, and no search exists
// without it. Give more columns when one value scopes them all (a child and
// its parent row in the same organization).
//
// A hard tenant column (a column of Postgres.Scopes that is not in
// Postgres.ContextScopes) is covered by its scope (tenant rule 1). A context
// tenant column takes ScopeFunc, not Scope. Any other column, such as a path
// parameter (webhook_requests.webhook_id), only adds a required value: it
// covers no tenant column and no instance. Every column must be on an
// instance that is not on the nullable side of a LEFT JOIN. A zero value is
// an error at run time.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Scope(column Column, more ...Column) SearchOption { return option{} }

// ScopeOrNull is Scope for rows that every tenant shares: the condition is
// (column = value OR column IS NULL). The exemption file lists it.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func ScopeOrNull(column Column) SearchOption { return option{} }

// ScopeNull keeps only the rows that no tenant owns: column IS NULL. It takes
// no value. The exemption file lists it.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func ScopeNull(column Column) SearchOption { return option{} }

// ScopeFunc scopes a context tenant column: every page and count applies
// fn(ctx, column). fn must be the function that Postgres.ContextScopes in
// nanogen/project.go registers for the column, and column must be that
// context scope column. fn can return TRUE (an organization-wide read). A nil
// result is an error at run time.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func ScopeFunc[C Column, B any](fn func(context.Context, C) B, column C) SearchOption {
	return option{}
}

// Unscoped exempts one tenant column from the scope check. why is the reason
// that a reviewer reads. Another hard tenant column of the same instance must
// be covered, so a search is never unscoped as a whole. The exemption file
// lists the column with why.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Unscoped(column Column, why string) SearchOption { return option{} }

// Condition adds the condition that fn returns to every page and count
// (kind = 'standalone'). fn returns a boolean expression of Nanostack DB. A
// condition is not a tenant scope.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func Condition[B any](fn func() B) SearchOption { return option{} }

// Filter makes column a field of the query builder with one method per op.
// The values have the type of the model field.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Filter(column Column, ops ...Op) SearchOption { return option{} }

// FilterAs is Filter with values of type V, a named type over the type of the
// model field (an enum).
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func FilterAs[V any](column Column, ops ...Op) SearchOption { return option{} }

// FilterExpr makes the expression that expr returns a field named name. Use
// it when the column is not the value that the search filters on
// (host(ip_address)).
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func FilterExpr[E any](name string, expr func() E, ops ...Op) SearchOption { return option{} }

// FilterExists keeps the rows that have a row of child whose column passes the
// filter: EXISTS (SELECT 1 FROM child WHERE child.link = <key> AND child.column
// ...). link must match a declared foreign key to a covered instance, and the
// tenant columns of child must be covered like those of any other instance.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func FilterExists(child Table, link, column Column, ops ...Op) SearchOption { return option{} }

// Flag is a named condition that the query builder applies or negates:
// <name>.Is(bool). The API property is the snake case of name
// ("has_query_params").
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Flag[B any](name string, condition func() B) SearchOption { return option{} }

// GoOnly marks a filter that the API does not expose: Go callers set it, and
// the API check does not ask for a property.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func GoOnly(filter SearchOption) SearchOption { return option{} }

// FullText matches the full-text term of the query against the text columns:
// case-insensitive, with the wildcards of the term escaped. A row matches when
// one column contains the term. A blank term matches every row.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func FullText(columns ...Column) SearchOption { return option{} }

// FullTextExpr matches the full-text term against the text expressions that
// exprs return. With FullText, the term matches the columns and the
// expressions.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func FullTextExpr[E any](exprs ...func() E) SearchOption { return option{} }

// Op is a filter operator. Each op is one method of the field on the query
// builder, and binds the API filter property of its conventional name, or the
// names that API gives.
type Op struct{}

// API binds the operator to these API filter properties instead of the
// conventional name: nanorepo.In.API("signature_statuses"). For Range, the
// names are the two bounds, in the order of Range.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func (o Op) API(properties ...string) Op { return o }

// HalfOpen makes a Range include its lower bound and exclude its upper bound:
// >= and <.
func (o Op) HalfOpen() Op { return o }

// Exclusive makes a Range exclude both bounds: > and <.
func (o Op) Exclusive() Op { return o }

// Range filters a time column on two bounds, inclusive by default (>= and <=).
// names are the API properties of the lower and the upper bound; without
// names they are "<column>_since" and "<column>_until". One name binds only
// the lower bound. HalfOpen and Exclusive change which bounds are included.
// The time field of the query builder has Since (>=), Until (<=), After (>)
// and Before (<) for Go callers.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func Range(names ...string) Op { return Op{} }

// The operators of Filter, FilterAs, FilterExpr and FilterExists.
var (
	// Eq is Eq(v): column = v. API property: the field name ("enabled").
	Eq Op
	// In is In(v...): column IN (v...). From Go, no values match no row; the
	// API mapper reads an empty list as no filter. More than
	// db/search.MaxInValues (1000) values is an error. API property: the
	// plural of the field name ("ids").
	In Op
	// InFold is In with case folded on both sides: LOWER(column) IN
	// (LOWER(v)...). Text columns only. More than db/search.MaxInValues
	// (1000) values is an error. API property: the plural, as In.
	InFold Op
	// Contains is Contains(term): a case-insensitive substring, with the
	// wildcards of term escaped. Text columns only. A blank term matches
	// every row; a term longer than db/search.MaxTermLength (1000) is an
	// error. API property: "<field>_contains".
	Contains Op
	// ContainsAny is ContainsAny(terms...): Contains on each term, joined
	// with OR. From Go, no terms match no row; the API mapper reads an empty
	// list as no filter. Text columns only. More than db/search.MaxInValues
	// (1000) terms, or a term longer than db/search.MaxTermLength (1000), is
	// an error. API property: the plural of the field name ("names").
	ContainsAny Op
	// Present is Present(bool): column IS NOT NULL, or IS NULL. Nullable
	// columns only. API property: "has_<field>".
	Present Op
)

// Sort makes columns sortable. The API name of each sort is the column name.
// Every column must map to a field of Row.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func Sort(columns ...Column) SearchOption { return option{} }

// SortSpec is a sort that SortKey declares. It is a SearchOption.
type SortSpec struct{}

func (SortSpec) searchOption() {}

// SortKey makes column sortable, with the API names and the NULL placement
// that the methods of SortSpec give.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func SortKey(column Column) SortSpec { return SortSpec{} }

// API gives the sort_by values of the sort instead of the column name. Two
// names are two API values of one key with one cursor signature.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func (s SortSpec) API(names ...string) SortSpec { return s }

// NullsFirst puts NULL before every value, in both directions. The column
// must be nullable. Without NullsFirst or NullsLast, NULL comes last when
// ascending and first when descending.
func (s SortSpec) NullsFirst() SortSpec { return s }

// NullsLast puts NULL after every value, in both directions. The column must
// be nullable.
func (s SortSpec) NullsLast() SortSpec { return s }

// SortExpr makes the expression that expr returns sortable. field is the Row
// field that receives the value, and the API name is the snake case of field
// ("RequestCount" is "request_count"). The field type must be the Go type of
// the expression: a pointer when the expression can be NULL. A subquery in
// expr can only aggregate (COUNT, SUM, MIN, MAX).
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func SortExpr[E any](field string, expr func() E) SearchOption { return option{} }

// SortHandWritten declares a sort value that the declaration cannot express.
// It adds a constant to the sort enum. Only a hand-written repository method
// can serve it, so nano gen repo refuses it on a generated method.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func SortHandWritten(name string) SearchOption { return option{} }

// Tiebreak ends every order with columns, so that the order is total and a
// cursor is stable. The columns must cover a unique key of the table. Without
// Tiebreak, the primary key ends every order.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func Tiebreak(columns ...Column) SearchOption { return option{} }

// Direction is the direction of a default sort.
type Direction int

// The directions of DefaultSort and DefaultSortExpr.
const (
	// Asc sorts from the lowest value to the highest.
	Asc Direction = iota
	// Desc sorts from the highest value to the lowest.
	Desc
)

// DefaultSort is the order when the caller gives none. column must be
// sortable.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func DefaultSort(column Column, direction Direction) SearchOption { return option{} }

// DefaultSortExpr is the order when the caller gives none, on the Row field of
// a SortExpr.
//
//nolint:revive // the parameters document the call; nano gen repo reads the arguments from source
func DefaultSortExpr(field string, direction Direction) SearchOption { return option{} }

// DefaultLimit is the page size when the caller gives none. With API, the
// default of pagination.limit in the request body applies, and both must agree
// when both are given.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func DefaultLimit(n int32) SearchOption { return option{} }

// API generates the mapper from the request body type Body to the query
// builder. nano gen repo checks the declaration against Body and the OpenAPI
// spec both ways: every filter property, every sort_by value and
// full_text_search. A body property that the mapper does not read is an
// error, unless IgnoresFullText or APIHandWritten names it.
func API[Body any]() SearchOption { return option{} }

// IgnoresFullText accepts the full_text_search property of the request body
// and ignores it. The property must come from the shared base schema, not from
// the schema of the endpoint. why is the reason that a reviewer reads, and the
// exemption file lists it.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func IgnoresFullText(why string) SearchOption { return option{} }

// APIHandWritten names the body properties that the service resolves before
// it filters: the generated mapper skips them, and the service sets the
// matching GoOnly filter. The exemption file lists each property.
//
//nolint:revive // the parameter documents the call; nano gen repo reads the argument from source
func APIHandWritten(properties ...string) SearchOption { return option{} }
