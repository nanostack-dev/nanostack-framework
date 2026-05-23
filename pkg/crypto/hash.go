package crypto

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashSHA256String returns the hex-encoded SHA-256 digest of input.
func HashSHA256String(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// CompareSHA256Hash compares input against a hex-encoded SHA-256 digest.
func CompareSHA256Hash(input, hashed string) bool {
	computed := HashSHA256String(input)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hashed)) == 1
}
