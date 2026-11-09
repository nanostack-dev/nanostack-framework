package genrepo

import (
	"strings"
	"unicode"
)

// words splits PascalCase with initialisms: PlatformTenantID -> Platform
// Tenant ID, APIKeys -> API Keys.
func words(s string) []string {
	var out []string
	r := []rune(s)
	start := 0
	for i := 1; i < len(r); i++ {
		if unicode.IsUpper(r[i]) && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]) ||
			(i+1 < len(r) && unicode.IsLower(r[i+1]))) {
			out = append(out, string(r[start:i]))
			start = i
		}
	}
	return append(out, string(r[start:]))
}

// lowerFirst lower-cases the first word: ProductUsers -> productUsers,
// APIKeys -> apiKeys.
func lowerFirst(s string) string {
	w := words(s)
	w[0] = strings.ToLower(w[0])
	return strings.Join(w, "")
}

func snake(s string) string {
	w := words(s)
	for i := range w {
		w[i] = strings.ToLower(w[i])
	}
	return strings.Join(w, "_")
}
