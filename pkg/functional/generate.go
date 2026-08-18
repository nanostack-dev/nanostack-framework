package functional

//go:generate go run ./internal/gen

// firstErr returns the leftmost non-nil error, or nil when every error is nil.
// The generated Zip helpers use it to decide whether any of their inputs
// failed before they look at presence: a failure has to outrank absence, since
// a caller that checks Err first must not see a real failure reported as a
// merely missing value.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
