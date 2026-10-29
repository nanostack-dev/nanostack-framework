package validate

import (
	"reflect"
	"regexp"

	"github.com/go-playground/validator/v10"
)

var (
	upperPattern   = regexp.MustCompile(`[A-Z]`)
	lowerPattern   = regexp.MustCompile(`[a-z]`)
	digitPattern   = regexp.MustCompile(`\d`)
	specialPattern = regexp.MustCompile(`[!@#$%^&*()\-+_=\[\]{};':"\\|,.<>\/?]+`)
)

// IsStrongPassword checks password complexity criteria.
func IsStrongPassword(fl validator.FieldLevel) bool {
	password, ok := reflect.TypeAssert[string](fl.Field())
	if !ok {
		return false
	}
	return IsPasswordStrong(password)
}

// IsPasswordStrong verifies password criteria statically.
func IsPasswordStrong(password string) bool {
	return upperPattern.MatchString(password) &&
		lowerPattern.MatchString(password) &&
		digitPattern.MatchString(password) &&
		specialPattern.MatchString(password)
}
