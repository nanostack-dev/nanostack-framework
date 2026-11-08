package validate_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
)

func TestPointerTokens(t *testing.T) {
	for raw, escaped := range map[string]string{
		"":        "",
		"limit":   "limit",
		"a/b":     "a~1b",
		"m~n":     "m~0n",
		"~1":      "~01",
		"/~/":     "~1~0~1",
		"x-trace": "x-trace",
	} {
		if got := validate.EscapeToken(raw); got != escaped {
			t.Errorf("EscapeToken(%q) = %q, want %q", raw, got, escaped)
		}
		if got := validate.UnescapeToken(escaped); got != raw {
			t.Errorf("UnescapeToken(%q) = %q, want %q", escaped, got, raw)
		}
	}
}
