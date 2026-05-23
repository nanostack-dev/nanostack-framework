package secrets

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestVersionedCipherRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	cipher, err := NewVersionedCipherWithSingleKey("v1", "test", key)
	if err != nil {
		t.Fatalf("NewVersionedCipherWithSingleKey returned error: %v", err)
	}
	encrypted, err := cipher.EncryptString("secret")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if !strings.HasPrefix(encrypted, "enc:v1:") {
		t.Fatalf("unexpected encrypted prefix: %q", encrypted)
	}
	decrypted, err := cipher.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "secret" {
		t.Fatalf("expected secret, got %q", decrypted)
	}
}
