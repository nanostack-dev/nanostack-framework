package ids

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	id, err := New("user")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if !strings.HasPrefix(id, "user_") {
		t.Fatalf("expected user_ prefix, got %q", id)
	}
}

func TestValidatePrefix(t *testing.T) {
	for _, prefix := range []string{"user", "user-name", "user_name", "user_name_v2"} {
		if err := ValidatePrefix(prefix); err != nil {
			t.Fatalf("expected valid prefix %q, got %v", prefix, err)
		}
	}

	for _, prefix := range []string{"", "User", "user.name"} {
		if err := ValidatePrefix(prefix); err == nil {
			t.Fatalf("expected invalid prefix %q", prefix)
		}
	}
}
