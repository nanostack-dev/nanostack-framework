package secrets

import "testing"

func TestPrefixedSpec(t *testing.T) {
	spec := PrefixedSpec{Prefix: "test_", RandomLength: 12}
	value, err := spec.Generate()
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if !spec.Validate(value) {
		t.Fatalf("expected generated value to validate: %q", value)
	}
	if spec.Validate(value + "x") {
		t.Fatal("expected modified value to fail validation")
	}
	if got := len(value); got != spec.Length() {
		t.Fatalf("expected length %d, got %d", spec.Length(), got)
	}
}

func TestHash(t *testing.T) {
	hash := Hash("secret")
	if !CompareHash("secret", hash) {
		t.Fatal("expected secret hash to match")
	}
}
