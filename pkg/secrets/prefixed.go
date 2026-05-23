package secrets

import (
	"crypto/rand"
	"fmt"
	"hash/crc32"
	"math/big"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/pkg/crypto"
)

const (
	DefaultRandomLength = 48
	ChecksumLength      = 8
	defaultAlphabet     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// PrefixedSpec configures a generated secret token with typo-detecting checksum.
type PrefixedSpec struct {
	Prefix       string
	RandomLength int
	Alphabet     string
}

func (s PrefixedSpec) normalize() PrefixedSpec {
	if s.RandomLength == 0 {
		s.RandomLength = DefaultRandomLength
	}
	if s.Alphabet == "" {
		s.Alphabet = defaultAlphabet
	}
	return s
}

// Generate creates a token in the form <prefix><random>_<crc32>.
func (s PrefixedSpec) Generate() (string, error) {
	s = s.normalize()
	if s.Prefix == "" {
		return "", fmt.Errorf("secret prefix is required")
	}
	if s.RandomLength <= 0 {
		return "", fmt.Errorf("random length must be positive")
	}
	if s.Alphabet == "" {
		return "", fmt.Errorf("alphabet is required")
	}

	bytes := make([]byte, s.RandomLength)
	for i := range bytes {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(s.Alphabet))))
		if err != nil {
			return "", fmt.Errorf("generate random secret character: %w", err)
		}
		bytes[i] = s.Alphabet[n.Int64()]
	}

	checksum := fmt.Sprintf("%08x", crc32.ChecksumIEEE(bytes))
	return s.Prefix + string(bytes) + "_" + checksum, nil
}

// Validate reports whether value matches the configured prefix and checksum.
func (s PrefixedSpec) Validate(value string) bool {
	s = s.normalize()
	if s.Prefix == "" || !strings.HasPrefix(value, s.Prefix) {
		return false
	}
	if len(value) != s.Length() {
		return false
	}
	separatorIndex := len(value) - ChecksumLength - 1
	if separatorIndex <= len(s.Prefix) || value[separatorIndex] != '_' {
		return false
	}
	randomPart := value[len(s.Prefix):separatorIndex]
	checksum := value[len(value)-ChecksumLength:]
	expected := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(randomPart)))
	return strings.EqualFold(checksum, expected)
}

// Length returns the exact token length for this spec.
func (s PrefixedSpec) Length() int {
	s = s.normalize()
	return len(s.Prefix) + s.RandomLength + 1 + ChecksumLength
}

// Obfuscate keeps the configured prefix and checksum suffix visible.
func (s PrefixedSpec) Obfuscate(value string) string {
	s = s.normalize()
	if len(value) < len(s.Prefix)+ChecksumLength+1 || !strings.HasPrefix(value, s.Prefix) {
		return Obfuscate(value)
	}
	return s.Prefix + "***" + value[len(value)-ChecksumLength-1:]
}

// Hash stores a one-way hash of a secret token.
func Hash(value string) string {
	return crypto.HashSHA256String(value)
}

// CompareHash compares a secret token against a stored hash.
func CompareHash(value, hash string) bool {
	return crypto.CompareSHA256Hash(value, hash)
}
