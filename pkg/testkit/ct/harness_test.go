package ct

import "testing"

func TestMustFreePort(t *testing.T) {
	if port := MustFreePort(); port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}
}
