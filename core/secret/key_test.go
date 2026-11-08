package secret_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestIsKey(t *testing.T) {
	t.Parallel()
	secrets := []string{
		"password", "Password", "db_password", "passwd", "token", "access_token", "refreshToken",
		"X-Api-Key", "api_key", "apiKey", "authorization", "Authorization", "cookie", "Set-Cookie",
		"client_secret", "clientSecret", "private_key", "credentials", "session.token",
		"secret_key", "SECRET_KEY", "aws_secret_access_key", "AWS_SECRET_ACCESS_KEY", "access_key",
		"signing_key", "encryption_key", "hmac_key", "client_key", "session_key", "master_key",
		"X-Hub-Signature", "dsn", "DATABASE_URL", "database_url", "db_url", "connection_string",
	}
	public := []string{
		"api_key_id", "api_key_name", "token_count", "max_tokens", "input_tokens", "secret_name",
		"password_reset_at", "org_id", "user_email", "", "key", "status",
		"key_id", "secret_key_id", "access_key_id", "public_key", "cache_key", "idempotency_key",
		"object_key", "database", "url",
	}
	for _, key := range secrets {
		if !secret.IsKey(key) {
			t.Errorf("IsKey(%q) = false", key)
		}
	}
	for _, key := range public {
		if secret.IsKey(key) {
			t.Errorf("IsKey(%q) = true", key)
		}
	}
}
