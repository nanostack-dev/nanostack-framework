package validate

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	"github.com/go-playground/validator/v10/non-standard/validators"
	en_translations "github.com/go-playground/validator/v10/translations/en"
)

// StructValidator encapsulates go-playground/validator instance with custom rules and translations.
type StructValidator struct {
	val        *validator.Validate
	translator ut.Translator
}

// NewStructValidator constructs and registers custom validations and translations.
func NewStructValidator() (*StructValidator, error) {
	v := validator.New()

	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	enLocal := en.New()
	uni := ut.New(enLocal, enLocal)
	translator, found := uni.GetTranslator("en")
	if !found {
		return nil, fmt.Errorf("translator not found")
	}

	err := en_translations.RegisterDefaultTranslations(v, translator)
	if err != nil {
		return nil, fmt.Errorf("failed to register default translations: %w", err)
	}

	// 1. Register strongpassword
	if err := v.RegisterValidation("strongpassword", IsStrongPassword); err != nil {
		return nil, fmt.Errorf("failed to register strongpassword validation: %w", err)
	}
	if err := v.RegisterTranslation("strongpassword", translator, func(ut ut.Translator) error {
		return ut.Add("strongpassword", "Password must contain at least one uppercase letter, one lowercase letter, one digit, and one special character", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		t, err := ut.T("strongpassword", fe.Field(), fe.Tag(), fe.Param())
		if err != nil {
			return fe.Error()
		}
		return t
	}); err != nil {
		return nil, fmt.Errorf("failed to register strongpassword translation: %w", err)
	}

	// 2. Register notblank
	if err := v.RegisterValidation("notblank", validators.NotBlank); err != nil {
		return nil, fmt.Errorf("failed to register notblank validation: %w", err)
	}
	if err := v.RegisterTranslation("notblank", translator, func(ut ut.Translator) error {
		return ut.Add("notblank", "{0} cannot be blank", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		t, err := ut.T("notblank", fe.Field())
		if err != nil {
			return fe.Error()
		}
		return t
	}); err != nil {
		return nil, fmt.Errorf("failed to register notblank translation: %w", err)
	}

	// 3. Register permission_name
	if err := v.RegisterValidation("permission_name", IsValidPermissionName); err != nil {
		return nil, fmt.Errorf("failed to register permission_name validation: %w", err)
	}
	if err := v.RegisterTranslation("permission_name", translator, func(ut ut.Translator) error {
		return ut.Add("permission_name", "{0} must follow the format 'resource:action' using only lowercase letters, numbers, dashes, and underscores", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		t, err := ut.T("permission_name", fe.Field())
		if err != nil {
			return fe.Error()
		}
		return t
	}); err != nil {
		return nil, fmt.Errorf("failed to register permission_name translation: %w", err)
	}

	// 4. Register request_url
	if err := v.RegisterValidation("request_url", IsValidRequestURL); err != nil {
		return nil, fmt.Errorf("failed to register request_url validation: %w", err)
	}
	if err := v.RegisterTranslation("request_url", translator, func(ut ut.Translator) error {
		return ut.Add("request_url", "{0} must be a valid URL or use a template variable like [baseUrl]/path", false)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		t, err := ut.T("request_url", fe.Field())
		if err != nil {
			return fe.Error()
		}
		return t
	}); err != nil {
		return nil, fmt.Errorf("failed to register request_url translation: %w", err)
	}

	return &StructValidator{
		val:        v,
		translator: translator,
	}, nil
}
