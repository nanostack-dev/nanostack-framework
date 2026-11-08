package secret

// secretSuffixes are the key endings that mark a credential. A key matches when,
// ignoring case and the separators '_', '-', '.' and ' ', it ends with one of
// them: "password", "access_token", "X-Api-Key", "Set-Cookie", "clientSecret",
// "AWS_SECRET_ACCESS_KEY" and "database_url" match. Matching the ending rather
// than any substring keeps identifiers and metadata about a credential, such
// as "api_key_id", "access_key_id", "token_count", "max_tokens" and
// "secret_name", visible.
//
// A bare "key" is not listed: "cache_key", "idempotency_key" and "object_key"
// are not secrets. Only the compound names below are.
var secretSuffixes = []string{ //nolint:gochecknoglobals // read-only table.
	"password", "passwords", "passwd", "passphrase",
	"secret", "secrets",
	"token", // not "tokens": "max_tokens" and "input_tokens" are counts.
	"apikey", "apikeys",
	"authorization",
	"cookie", "cookies",
	"credential", "credentials",
	"signature",
	// Keys: "secret_key", "aws_secret_access_key", "signing_key", ...
	"privatekey", "secretkey", "accesskey", "signingkey", "encryptionkey",
	"hmackey", "clientkey", "sessionkey", "masterkey",
	// Connection strings, which usually embed a password.
	"dsn", "connectionstring", "databaseurl", "dburl",
}

// IsKey reports whether a name looks like it names a credential: a log
// attribute key, a URL query parameter or a DSN keyword. core/log redacts the
// value of every such attribute at any group depth, and of every such query
// parameter in the request summary; RedactURL redacts such query parameters.
// It is a guard against mistakes, not the primary control: hold secrets as
// Secret.
func IsKey(key string) bool {
	for _, suffix := range secretSuffixes {
		if hasFoldedSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// hasFoldedSuffix reports whether key, with separators removed and ASCII
// letters lowered, ends with suffix, which must be lowercase without
// separators. It does not allocate.
func hasFoldedSuffix(key, suffix string) bool {
	i := len(key) - 1
	for j := len(suffix) - 1; j >= 0; j-- {
		for i >= 0 && isKeySeparator(key[i]) {
			i--
		}
		if i < 0 {
			return false
		}
		c := key[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != suffix[j] {
			return false
		}
		i--
	}
	return true
}

func isKeySeparator(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c == ' '
}
