package secret_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestRedactText(t *testing.T) {
	for in, want := range map[string]string{
		"":                                  "",
		"plain":                             "plain",
		"ops@example.com":                   "ops@example.com",
		"https://example.com/a":             "https://example.com/a",
		"https://user@example.com":          "https://user@example.com",
		"https://host:8080/x":               "https://host:8080/x",
		"postgres://app:pw@db/app":          "postgres://app:[REDACTED]@db/app",
		"postgres://app:pw@db:port/app":     "postgres://app:[REDACTED]@db:port/app",
		"postgres://app:pw@/app":            "postgres://app:[REDACTED]@/app",
		"postgres://app:p/w?#@db/app":       "postgres://app:[REDACTED]@db/app",
		"postgres://app:p@ss@db/app":        "postgres://app:[REDACTED]@db/app",
		"redis://:pw@cache:6379/0 and more": "redis://:[REDACTED]@cache:6379/0 and more",
		"a://u:p@h,b://v:q@i":               "a://u:[REDACTED]@i",
		// Malformed DSNs that a URL parser rejects or misreads.
		"app:Pw9SECRET@db:5432/app":                            "app:[REDACTED]@db:5432/app",
		"postgres:/app:Pw9SECRET@db/app":                       "postgres:[REDACTED]@db/app",
		"postgres://app:my Pw9SECRET@db/app":                   "postgres://app:[REDACTED]@db/app",
		"postgres://app:Pw9SECRET\n@db/app":                    "postgres://app:[REDACTED]@db/app",
		"postgres://db/app?password=Pw9SECRET":                 "postgres://db/app?password=[REDACTED]",
		"host=db user=app password=Pw9SECRET dbname=app":       "host=db user=app password=[REDACTED] dbname=app",
		"host=db sslpassword='Pw9 SECRET' sslmode=verify-full": "host=db sslpassword=[REDACTED] sslmode=verify-full",
		"a=1&token=Pw9SECRET&b=2":                              "a=1&token=[REDACTED]&b=2",
		"max_tokens=100":                                       "max_tokens=100",
		// Double quotes, unclosed quotes and the bare "pass" keyword.
		`host=db password="a Pw9SECRET" sslmode=require`: "host=db password=[REDACTED] sslmode=require",
		`host=db password="a Pw9SECRET`:                  "host=db password=[REDACTED]",
		`host=db password='a Pw9SECRET`:                  "host=db password=[REDACTED]",
		"pass=Pw9SECRET":                                 "pass=[REDACTED]",
		"user=app db_pass=Pw9SECRET":                     "user=app db_pass=[REDACTED]",
		"Server=db;User Id=app;Password=Pw9SECRET;":      "Server=db;User Id=app;Password=[REDACTED];",
	} {
		if got := secret.RedactText(in); got != want {
			t.Errorf("RedactText(%q) = %q, want %q", in, got, want)
		}
	}
}
