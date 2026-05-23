package validate

import "github.com/nanostack-dev/nanostack-framework/pkg/apierror"

var defaultStructValidator = mustNewStructValidator() //nolint:gochecknoglobals // Shared default validator for bounded migration slices.

func mustNewStructValidator() *StructValidator {
	validator, err := NewStructValidator()
	if err != nil {
		panic("validate: failed to create default struct validator: " + err.Error())
	}
	return validator
}

// ValidateStruct validates a struct with the package default validator.
func ValidateStruct(s interface{}) *apierror.Error {
	return defaultStructValidator.Validate(s)
}
