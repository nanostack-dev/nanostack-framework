package validate

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/nanostack-dev/nanostack-framework/pkg/apierror"
)

// Validate validates a struct and returns an API-safe apierror.Error on failure.
func (sv *StructValidator) Validate(s interface{}) *apierror.Error {
	if sv == nil || sv.val == nil {
		return apierror.NewWithStatus("VALIDATION_SETUP_ERROR", "Validator is not initialized", http.StatusInternalServerError)
	}

	err := sv.val.Struct(s)
	if err == nil {
		return nil
	}

	var validationErrors validator.ValidationErrors
	if errors.As(err, &validationErrors) {
		details := make([]apierror.Detail, 0, len(validationErrors))
		for _, fieldError := range validationErrors {
			fieldName := fieldError.Field()
			details = append(details, apierror.Detail{
				Code:    "VALIDATION_ERROR",
				Message: fieldError.Translate(sv.translator),
				Metadata: map[string]any{
					"field": fieldName,
					"rule":  fieldError.Tag(),
					"param": fieldError.Param(),
					"value": fieldError.Value(),
				},
			})
		}
		return &apierror.Error{
			Details: details,
			Status:  http.StatusBadRequest,
		}
	}

	var invalidValidationError *validator.InvalidValidationError
	if errors.As(err, &invalidValidationError) {
		return apierror.NewWithStatus("VALIDATION_SETUP_ERROR", "Invalid input provided for validation", http.StatusInternalServerError)
	}

	return apierror.NewWithStatus("UNEXPECTED_VALIDATION_ERROR", fmt.Sprintf("An unexpected error occurred during validation: %s", err.Error()), http.StatusInternalServerError)
}
