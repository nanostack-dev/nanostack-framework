package secret_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestRedactURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"postgres://app:pw@db:5432/app":                         "postgres://app:[REDACTED]@db:5432/app",
		"postgres://app@db/app":                                 "postgres://app@db/app",
		"redis://:pw@cache:6379/0":                              "redis://:[REDACTED]@cache:6379/0",
		"postgres://a%40b:pw@db/app":                            "postgres://a%40b:[REDACTED]@db/app",
		"postgres://db/app?sslmode=require&sslpassword=pw":      "postgres://db/app?sslmode=require&sslpassword=[REDACTED]",
		"postgres://db/app?password=pw&password=pw2&user=app":   "postgres://db/app?password=[REDACTED]&password=[REDACTED]&user=app",
		"mysql://u:pw@db/app?Password=pw&api%5Fkey=k&token":     "mysql://u:[REDACTED]@db/app?Password=[REDACTED]&api%5Fkey=[REDACTED]&token",
		"https://example.com/cb?access_token=t&state=s#frag":    "https://example.com/cb?access_token=[REDACTED]&state=s#frag",
		"https://example.com/search?q=password&max_tokens=1000": "https://example.com/search?q=password&max_tokens=1000",
		"//app:pw@db/app":                                       "//app:[REDACTED]@db/app",
	} {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatalf("parse %q: %v", in, err)
		}
		if got := secret.RedactURL(u); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := secret.RedactURL(nil); got != "" {
		t.Errorf("RedactURL(nil) = %q, want empty", got)
	}
}

// A password that is not percent-encoded can make url.Parse read the
// authority wrongly: no userinfo, and the password in the host, the path or
// the fragment. RedactURL then hides everything from the first colon to the
// last '@', as RedactText does.
func TestRedactURLHidesAMisparsedPassword(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"postgres://app:#hunter2@db:5432/app",
		"postgres://app:?hunter2@db/app",
		"postgres://app:/hunter2@db/app",
		"postgres://app:1234/hunter2@db:5432/app",
		"redis://:#hunter2@cache",
	} {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatalf("parse %q: %v", in, err)
		}
		if got := secret.RedactURL(u); strings.Contains(got, "hunter2") {
			t.Errorf("RedactURL(%q) = %q, which shows the password", in, got)
		}
	}
	// An '@' in the query is an ordinary value, not a misparse.
	u, _ := url.Parse("https://host:8080/x?email=a@b.c")
	if got := secret.RedactURL(u); got != "https://host:8080/x?email=a@b.c" {
		t.Errorf("RedactURL = %q, want the URL unchanged", got)
	}
}

func TestRedactURLs(t *testing.T) {
	a, _ := url.Parse("postgres://app:pw@db/app")
	b, _ := url.Parse("https://example.com/x?token=t")
	got := secret.RedactURLs([]*url.URL{a, nil, b})
	want := []string{"postgres://app:[REDACTED]@db/app", "", "https://example.com/x?token=[REDACTED]"}
	if len(got) != len(want) {
		t.Fatalf("RedactURLs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RedactURLs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
