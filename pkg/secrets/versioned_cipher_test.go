package secrets

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewGCM(t *testing.T) {
	tests := []struct {
		name      string
		keySize   int
		wantErr   bool
		wantInMsg string
	}{
		{name: "valid AES-256 key", keySize: 32, wantErr: false},
		{name: "wrong key size", keySize: 15, wantErr: true, wantInMsg: "failed to initialize aes cipher"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gcm, err := newGCM(make([]byte, tt.keySize))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("newGCM returned no error for a %d-byte key", tt.keySize)
				}
				if !strings.Contains(err.Error(), tt.wantInMsg) {
					t.Fatalf("expected error to mention %q, got %q", tt.wantInMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("newGCM returned error: %v", err)
			}
			if gcm == nil {
				t.Fatal("newGCM returned a nil AEAD with no error")
			}
		})
	}
}

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
