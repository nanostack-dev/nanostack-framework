package ptr

import "testing"

func TestPtr(t *testing.T) {
	value := Ptr("value")
	if value == nil || *value != "value" {
		t.Fatalf("expected pointer to value, got %#v", value)
	}
}

func TestDerefOr(t *testing.T) {
	if got := DerefOr[string](nil, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}

	value := "runner failed"
	if got := DerefOr(&value, "fallback"); got != value {
		t.Fatalf("expected %q, got %q", value, got)
	}
}
