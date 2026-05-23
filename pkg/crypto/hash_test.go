package crypto

import "testing"

func TestCompareSHA256Hash(t *testing.T) {
	hashed := HashSHA256String("secret")
	if !CompareSHA256Hash("secret", hashed) {
		t.Fatal("expected hash comparison to succeed")
	}
	if CompareSHA256Hash("other", hashed) {
		t.Fatal("expected hash comparison to fail")
	}
}
