// Package searchexec is the runtime of the searches that nano gen repo writes. The
// generated code and hand-written repositories call it; the package is not a query
// language of its own, and every statement stays a Nanostack DB (go-jet) statement.
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
package searchexec
