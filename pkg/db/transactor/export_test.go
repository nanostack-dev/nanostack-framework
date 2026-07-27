package transactor

// NewResultForTest builds a Result the way the query helpers do, so the
// external test package can exercise error translation without a database.
// The testpackage linter skips export_test.go by design.
func NewResultForTest[T any](v T, err error) Result[T] {
	return newResult(v, err)
}
