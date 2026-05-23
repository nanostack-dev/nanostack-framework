package validate

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

var templatePattern = regexp.MustCompile(`\{\{[^}]+\}\}`)

// IsValidRequestURL checks URL validity, including dynamic template placeholders.
func IsValidRequestURL(fl validator.FieldLevel) bool {
	value, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}
	return IsRequestURL(value)
}

// IsRequestURL verifies request URL criteria statically.
func IsRequestURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}

	normalized := templatePattern.ReplaceAllString(trimmed, "https://placeholder.local")
	if normalized == "https://placeholder.local" {
		return true
	}

	parsed, err := url.Parse(normalized)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return true
	}

	return strings.HasPrefix(normalized, "/")
}
