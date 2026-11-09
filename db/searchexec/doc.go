// Package searchexec is the runtime of the searches that nano gen repo writes. The
// generated code and hand-written repositories call it; the package is not a query
// language of its own, and every statement stays a Nanostack DB (go-jet) statement.
//
// # Generated searches
//
// nano gen repo writes three things per declared search: a Spec, a builder and the
// repository method that runs it.
//
//   - The Spec is a package variable that DefineSpec returns. It holds the source (From), the
//     projection, the tenant scope, the fixed conditions, the sort keys, the tiebreak, the
//     default sort and the default limit. DefineSpec panics at package init when the search
//     has no source, no key, no tiebreak or no tenant scope.
//   - The builder is a struct that embeds Query and nothing else. Query gives it Scope,
//     SortBy, After, Offset and Limit. Every method returns a changed copy, so a builder is
//     immutable. The scope is a typed value P: one value, or a generated scope struct whose
//     fields must be named. A zero scope field refuses the search, also a field that only
//     From reads.
//   - The repository method runs the builder through the transactor forwarders
//     (transactor.Search and SearchPage, in a later release), which use the context
//     transaction and refuse a row lock. A hand-written method starts from Resolve, which
//     returns an opaque Plan: the method can narrow it with Plan.And, add the keys of its
//     hand-written sorts with Plan.Order, and run it through transactor.RunPlan or
//     RunPlanPage (in the same release).
//
// Run, RunPage, Plan.Run and Plan.RunPage run on the executor they get. They do not read
// the context transaction and do not refuse a row lock, so repository code does not call
// them directly.
//
// Resolve, Run and RunPage take the spec as an argument and refuse a query whose spec is
// another one, a struct literal of the builder, a zero scope value and a context scope that
// returns nil, all with search.ErrUnscoped. They also refuse a page that search.CheckPage
// refuses. No statement runs before these checks pass, and the page always runs with
// keyset.PageBuilder.RequireWhere.
//
// The tenant conditions are ScopeEq (col = $1, for one or more columns), ScopeOrNull
// ((col = $1 OR col IS NULL), for rows that every tenant shares), ScopeNull (col IS NULL),
// ScopeFrom (a scope field that From puts in an ON clause; it adds no WHERE condition) and
// ScopeFunc (a condition that the context carries, such as the workspace).
//
// The generated searches support PostgreSQL. A source of another dialect is
// keyset.ErrDialect.
//
// # Field types
//
// Each filter method of a generated builder returns a field of the query, and the method of
// the field returns the filtered copy:
//
//	q.Method().In("GET", "POST").ReceivedAt().Since(t)
//
// A field has only the operators that its declaration names:
//
//	In         In(v...)        expr IN (v...)
//	InFold     In(v...)        expr ILIKE v OR ..., v escaped: exact, folded in the database
//	Eq         Eq(v)           expr = v
//	EqIn       Eq(v), In(v...)
//	TimeRange  Since(t), Until(t), After(t), Before(t)    >=, <=, >, <
//	Presence   Present(bool)   expr IS NOT NULL, or expr IS NULL
//	Flag       Is(bool)        condition, or condition IS NOT TRUE
//	Text       Contains(term), ContainsAny(terms...)
//	Exists     In(v...)        EXISTS (SELECT 1 FROM child WHERE correlation AND column IN (v...))
//	                           (NewExistsFold: column ILIKE v OR ..., as InFold)
//
// From Go, an empty list matches no row: a caller that builds an empty list gets nothing,
// not everything. The API mapper reads an empty list as no filter and does not call the
// field. InFold leaves the case of the values to the database, because Go and the
// database fold some letters differently (the Turkish dotted I under COLLATE "C"). ILIKE
// folds the value by the collation of the column, so the answer agrees with the column
// also when its collation is not the default one. LOWER($1) would fold the value by the
// default collation. On the other dialects InFold is LOWER(expr) LIKE LOWER(?) ESCAPE '!'.
//
// A string value that no text column can hold (not valid UTF-8, or holding NUL) matches no
// row in every field, as in Contains: StringLit makes it NULL and InFold drops it. A
// TimeRange bound moves to a whole microsecond, the precision of timestamptz, in the
// direction that keeps its meaning, so lib/pq and pgx give the same rows.
//
// A field does not return an error. More than search.MaxInValues values
// (search.ErrTooManyValues), a term longer than search.MaxTermLength characters after the
// trim (search.ErrTermTooLong), or a child table of another dialect (keyset.ErrDialect)
// records the error on the query. Resolve returns the first recorded error after the scope
// checks, so a missing tenant scope still wins, and no statement runs.
//
// # Contains
//
// Contains is the one substring rule of the framework. A search's full-text filter, each
// text field and the repository base's XContains predicates use it, so they match the same
// rows. It trims the term, escapes the LIKE wildcards with '!', and folds the case in the
// database:
//
//	PostgreSQL (and CockroachDB)  expr ILIKE $1 ESCAPE '!'
//	MySQL, MariaDB and SQLite      LOWER(expr) LIKE LOWER(?) ESCAPE '!'
//
// The statement's dialect selects the SQL when the statement is serialized, so one value
// runs on every dialect.
//
// Matching follows the collation of the column:
//
//   - SQLite's LOWER and LIKE fold the case of ASCII letters only, so on SQLite the term
//     "é" does not find "É". A product that needs Unicode case folding on SQLite loads the
//     ICU extension; the framework does not ship it.
//   - PostgreSQL's ILIKE folds the case through the ctype of the column's collation. A libc
//     locale such as en_US.UTF-8, an ICU collation and the builtin C.UTF-8 (PostgreSQL 17
//     and later) fold every letter. The "C" and "POSIX" collations fold ASCII letters only,
//     as SQLite does. This is also true for every column of a database made with
//     --locale=C. MySQL and MariaDB fold every letter.
//   - CockroachDB's ILIKE and its LOWER do not agree on the Turkish I: under ILIKE the
//     dotless ı matches i, and İ matches only İ. Contains and InFold follow its ILIKE.
//   - On PostgreSQL, ILIKE refuses a column with a nondeterministic collation with an SQL
//     error. Give such a column an expression with an explicit deterministic COLLATE.
//   - On MySQL and MariaDB, an accent-insensitive collation (utf8mb4_0900_ai_ci,
//     utf8mb4_uca1400_ai_ci, utf8mb4_general_ci) also ignores accents, so the term "e"
//     finds "é". On PostgreSQL and SQLite it does not.
//   - On MySQL and MariaDB, a column with a _bin or _cs collation still matches without
//     case, because both sides are folded. On PostgreSQL, a column in "C" folds ASCII
//     letters only.
//
// A term that is not valid UTF-8, or that holds a NUL character, matches no row on every
// dialect.
//
// # API helpers
//
// The generated <Name>SearchFromAPI turns an API request body into a builder with Some,
// Opt, OrZero, APISort, APIDirection and APIPage. They apply the rules of the API
// contract: a property that is not sent, and an empty list, filter nothing.
//
//	q, b := NewWebhookRequestSearch(scope), searchexec.OrZero(body)
//	if f := b.Filter; f != nil {
//		q = searchexec.Some(q, q.Method().In, f.Methods)
//		q = searchexec.Opt(q, q.ReceivedAt().Since, f.Since)
//	}
//	q = searchexec.Opt(q, q.FullText, b.FullTextSearch)
//	q = searchexec.APISort(q, q.SortBy, b.SortBy, b.SortDirection,
//		WebhookRequestSortReceivedAt, search.SortDescending)
//	return searchexec.APIPage(q.Query, b.Pagination)
//
// APISort sorts a sort_by sent alone in the direction its last argument gives, because each
// API contract states its own, and applies a sort_direction alone to the default sort.
// APIDirection does the same for a body that has sort_direction and no sort_by. APIPage
// keeps the spec's default limit when the request sends no limit (zero). The helpers do no
// checks of their own: Resolve, Run and RunPage refuse an unknown sort, a sort direction
// other than ASC or DESC, a page outside the limits and too many values, as they do for a
// Go caller.
package searchexec
