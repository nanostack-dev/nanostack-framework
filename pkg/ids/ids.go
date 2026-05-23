package ids

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/segmentio/ksuid"
)

var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ValidatePrefix checks the Nanostack public ID prefix format.
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return errors.New("prefix cannot be empty")
	}
	if !prefixPattern.MatchString(prefix) {
		return fmt.Errorf("prefix %q must be lowercase and contain only letters, numbers, dashes, or underscores", prefix)
	}
	return nil
}

// New creates a KSUID-backed public identifier in the form <prefix>_<ksuid>.
func New(prefix string) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, ksuid.New().String()), nil
}

// MustNew creates a KSUID-backed public identifier or panics on an invalid prefix.
func MustNew(prefix string) string {
	id, err := New(prefix)
	if err != nil {
		panic(err)
	}
	return id
}
