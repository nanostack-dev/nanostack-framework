package validate

import (
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

const expectedPermissionParts = 2

var permissionPartPattern = regexp.MustCompile(`^[a-z0-9-_]+$`)

// IsValidPermissionName validates permission names in the resource:action format.
func IsValidPermissionName(fl validator.FieldLevel) bool {
	permissionName, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}
	return IsPermissionName(permissionName)
}

// IsPermissionName validates a raw permission name without requiring validator.FieldLevel.
func IsPermissionName(permissionName string) bool {
	parts := strings.Split(permissionName, ":")
	if len(parts) != expectedPermissionParts {
		return false
	}
	resource := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	if resource == "" || action == "" {
		return false
	}
	return permissionPartPattern.MatchString(resource) && permissionPartPattern.MatchString(action)
}
