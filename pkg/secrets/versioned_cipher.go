package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	versionedSecretPrefix = "enc"
	keyLengthAES256       = 32
	aadSeparator          = ":"
	versionPayloadParts   = 3
	keyEntryParts         = 2
)

// VersionedCipherConfig configures a versioned AES-GCM secret cipher.
type VersionedCipherConfig struct {
	CurrentVersion string
	Context        string
	Base64Keys     map[string]string
}

// VersionedCipher encrypts and decrypts secrets with versioned key support.
type VersionedCipher struct {
	currentVersion string
	context        string
	keys           map[string][]byte
}

// NewVersionedCipher creates a versioned cipher from base64-encoded AES-256 keys.
func NewVersionedCipher(cfg VersionedCipherConfig) (*VersionedCipher, error) {
	if strings.TrimSpace(cfg.CurrentVersion) == "" {
		return nil, errors.New("current version is required")
	}
	if len(cfg.Base64Keys) == 0 {
		return nil, errors.New("at least one key is required")
	}

	decodedKeys := make(map[string][]byte, len(cfg.Base64Keys))
	for version, rawKey := range cfg.Base64Keys {
		trimmedVersion := strings.TrimSpace(version)
		if trimmedVersion == "" {
			return nil, errors.New("key version cannot be empty")
		}
		decodedKey, err := decodeBase64Key(rawKey)
		if err != nil {
			return nil, fmt.Errorf("invalid key for version %q: %w", trimmedVersion, err)
		}
		if len(decodedKey) != keyLengthAES256 {
			return nil, fmt.Errorf("key for version %q must be %d bytes", trimmedVersion, keyLengthAES256)
		}
		decodedKeys[trimmedVersion] = decodedKey
	}

	currentVersion := strings.TrimSpace(cfg.CurrentVersion)
	if _, exists := decodedKeys[currentVersion]; !exists {
		return nil, fmt.Errorf("missing key for current version %q", currentVersion)
	}

	return &VersionedCipher{
		currentVersion: currentVersion,
		context:        strings.TrimSpace(cfg.Context),
		keys:           decodedKeys,
	}, nil
}

// NewVersionedCipherWithSingleKey builds a cipher from one versioned key.
func NewVersionedCipherWithSingleKey(version, context, base64Key string) (*VersionedCipher, error) {
	return NewVersionedCipher(VersionedCipherConfig{
		CurrentVersion: version,
		Context:        context,
		Base64Keys: map[string]string{
			version: base64Key,
		},
	})
}

// EncryptString encrypts plaintext and returns enc:<version>:<base64url(nonce|ciphertext)>.
func (c *VersionedCipher) EncryptString(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if IsVersionedEncryptedSecret(plaintext) {
		return "", errors.New("input matches encrypted token format and must not be re-encrypted")
	}

	key, exists := c.keys[c.currentVersion]
	if !exists {
		return "", fmt.Errorf("missing key for version %q", c.currentVersion)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to initialize aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to initialize aes-gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	cipherText := gcm.Seal(nil, nonce, []byte(plaintext), []byte(c.aad(c.currentVersion)))
	payload := make([]byte, 0, len(nonce)+len(cipherText))
	payload = append(payload, nonce...)
	payload = append(payload, cipherText...)
	return fmt.Sprintf(
		"%s:%s:%s",
		versionedSecretPrefix,
		c.currentVersion,
		base64.RawURLEncoding.EncodeToString(payload),
	), nil
}

// DecryptString decrypts a versioned token. Non-versioned input is returned unchanged.
func (c *VersionedCipher) DecryptString(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	parsed, ok := ParseVersionedEncryptedSecret(encrypted)
	if !ok {
		return encrypted, nil
	}

	key, exists := c.keys[parsed.Version]
	if !exists {
		return "", fmt.Errorf("missing key for version %q", parsed.Version)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parsed.Payload)
	if err != nil {
		return "", fmt.Errorf("invalid encrypted payload: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to initialize aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to initialize aes-gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", errors.New("invalid encrypted payload")
	}
	plainText, err := gcm.Open(nil, payload[:nonceSize], payload[nonceSize:], []byte(c.aad(parsed.Version)))
	if err != nil {
		return "", fmt.Errorf("failed to decrypt payload: %w", err)
	}
	return string(plainText), nil
}

// CurrentVersion returns the write version used for encryption.
func (c *VersionedCipher) CurrentVersion() string {
	return c.currentVersion
}

// VersionedEncryptedSecret is a parsed versioned secret token.
type VersionedEncryptedSecret struct {
	Version string
	Payload string
}

// ParseVersionedEncryptedSecret parses enc:<version>:<payload> formatted values.
func ParseVersionedEncryptedSecret(value string) (VersionedEncryptedSecret, bool) {
	parts := strings.SplitN(value, aadSeparator, versionPayloadParts)
	if len(parts) != versionPayloadParts || parts[0] != versionedSecretPrefix {
		return VersionedEncryptedSecret{}, false
	}
	version := strings.TrimSpace(parts[1])
	payload := strings.TrimSpace(parts[2])
	if version == "" || payload == "" || version != parts[1] || payload != parts[2] {
		return VersionedEncryptedSecret{}, false
	}
	return VersionedEncryptedSecret{Version: version, Payload: payload}, true
}

// IsVersionedEncryptedSecret reports whether a value is in versioned token format.
func IsVersionedEncryptedSecret(value string) bool {
	_, ok := ParseVersionedEncryptedSecret(value)
	return ok
}

// ParseVersionedBase64Keys parses comma-separated versioned keys like v1=<base64>,v2=<base64>.
func ParseVersionedBase64Keys(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("versioned key string is empty")
	}
	entries := strings.Split(trimmed, ",")
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		kv := strings.SplitN(strings.TrimSpace(entry), "=", keyEntryParts)
		if len(kv) != keyEntryParts {
			return nil, fmt.Errorf("invalid key entry %q", entry)
		}
		version := strings.TrimSpace(kv[0])
		base64Key := strings.TrimSpace(kv[1])
		if version == "" || base64Key == "" {
			return nil, fmt.Errorf("invalid key entry %q", entry)
		}
		if _, exists := result[version]; exists {
			return nil, fmt.Errorf("duplicate key version %q", version)
		}
		result[version] = base64Key
	}
	return result, nil
}

func (c *VersionedCipher) aad(version string) string {
	if c.context == "" {
		return version
	}
	return c.context + aadSeparator + version
}

func decodeBase64Key(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("key is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(trimmed)
	if rawErr == nil {
		return decoded, nil
	}
	return nil, err
}
