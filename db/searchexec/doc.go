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
//     "é" does not find "É". PostgreSQL and MySQL fold the case of every letter. A product
//     that needs Unicode case folding on SQLite loads the ICU extension; the framework does
//     not ship it.
//   - On MySQL and MariaDB, an accent-insensitive collation (utf8mb4_0900_ai_ci,
//     utf8mb4_uca1400_ai_ci, utf8mb4_general_ci) also ignores accents, so the term "e"
//     finds "é". On PostgreSQL and SQLite it does not.
//   - A column with a binary or case-sensitive collation still matches without case,
//     because both sides are folded.
package searchexec
