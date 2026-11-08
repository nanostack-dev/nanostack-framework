package config_test

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nanostack-dev/nanostack-framework/core/config"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// TestBoundSecretStaysHidden binds a secret setting and checks that the bound
// value is the framework's one Secret type, hidden from YAML as well as from
// the encoders that core/secret tests itself.
func TestBoundSecretStaysHidden(t *testing.T) {
	t.Setenv("API_KEY", "sk_live_123")
	src, err := config.NewSources(config.Env())
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(t.Context(), src)
	key, _ := config.Scalar(b, config.Key{Path: "api_key", Env: "API_KEY", Secret: true}, config.ParseSecret)
	if err = b.Err(); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(struct {
		Key secret.Secret `yaml:"key"`
	}{key})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "sk_live") || !strings.Contains(string(out), secret.Redacted) {
		t.Errorf("YAML: %s", out)
	}
	if key.Reveal() != "sk_live_123" {
		t.Errorf("Reveal() = %q", key.Reveal())
	}
}

func TestParseHelpers(t *testing.T) {
	if v, err := config.ParseInt[int8]("127"); err != nil || v != 127 {
		t.Errorf("ParseInt[int8](127) = %v, %v", v, err)
	}
	if _, err := config.ParseInt[int8]("128"); err == nil || err.Error() != "is out of range" {
		t.Errorf("ParseInt[int8](128) err = %v", err)
	}
	if _, err := config.ParseUint[uint]("-1"); err == nil {
		t.Error("ParseUint(-1) must fail")
	}
	if _, err := config.ParseFloat[float32]("1e300"); err == nil || err.Error() != "is out of range" {
		t.Errorf("ParseFloat[float32](1e300) err = %v", err)
	}
	if _, err := config.ParseFloat[float64]("NaN"); err == nil {
		t.Error("NaN must fail")
	}
	if v, err := config.ParseBool[bool]("TRUE"); err != nil || !v {
		t.Errorf("ParseBool = %v, %v", v, err)
	}
	if d, err := config.ParseDuration(" 1m30s "); err != nil || d != 90*time.Second {
		t.Errorf("ParseDuration = %v, %v", d, err)
	}
	// An empty port is the misparse of a password that is not
	// percent-encoded: postgres://app:#pw@db reads as host "app:".
	for _, bad := range []string{"/relative", "example.com", "https://", "postgres://app:#hunter2@db/app", "redis://:#pw@cache"} {
		if _, err := config.ParseURL(bad); err == nil {
			t.Errorf("ParseURL(%q) must fail", bad)
		}
	}
	// A scoped npm registry has an '@' in its path; it is a valid setting.
	if _, err := config.ParseURL("https://cdn.example/npm/@org/pkg"); err != nil {
		t.Errorf("ParseURL of a path with '@' = %v", err)
	}
	if a, err := config.ParseText[netip.Addr]("10.0.0.1"); err != nil || a.String() != "10.0.0.1" {
		t.Errorf("ParseText[netip.Addr] = %v, %v", a, err)
	}
	if _, err := config.ParseText[netip.Addr]("nope"); err == nil || err.Error() != "must be a valid netip.Addr" {
		t.Errorf("ParseText error = %v", err)
	}
	type mode string
	if m, _ := config.ParseString[mode]("live"); m != "live" {
		t.Error("ParseString on a named type")
	}
}

func TestBinderAccumulatesAndRedacts(t *testing.T) {
	src, err := config.NewSources(config.EnvMap(map[string]string{"PORT": "x", "KEY": "short", "EMPTY": ""}))
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(context.Background(), src)
	port := config.Key{Path: "port", Env: "PORT"}
	if _, ok := config.Scalar(b, port, config.ParseInt[int]); ok {
		t.Error("bad int must not bind")
	}
	key := config.Key{Path: "key", Env: "KEY", Secret: true}
	if v, ok := config.Scalar(b, key, config.ParseSecret); ok {
		config.OneOf(b, key, v.Reveal(), "long-enough")
	}
	empty := config.Key{Path: "empty", Env: "EMPTY", Default: "fallback"}
	if v, _ := config.Scalar(b, empty, config.ParseString[string]); v != "fallback" {
		t.Errorf("an empty env value counts as unset; got %q", v)
	}
	hosts := config.Key{Path: "hosts", Env: "HOSTS", Default: "a, ,b"}
	if v, _ := config.List(b, hosts, config.ParseString[string]); strings.Join(v, "|") != "a|b" {
		t.Errorf("List default split = %q", v)
	}
	err = b.Err()
	var cerr *config.Error
	if !errors.As(err, &cerr) || len(cerr.Issues) != 2 {
		t.Fatalf("err = %v", err)
	}
	if got := cerr.Issues[0].String(); got != `port (env PORT): must be an integer; got "x" from env PORT` {
		t.Errorf("issue 0 = %s", got)
	}
	if got := cerr.Issues[1].String(); strings.Contains(got, "short") || !strings.HasSuffix(got, "from env KEY") {
		t.Errorf("secret issue must hide the value: %s", got)
	}
}

func TestSecretProviderErrorIsAnIssue(t *testing.T) {
	src, err := config.NewSources(config.Secrets(failingStore{}))
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(context.Background(), src)
	config.Scalar(b, config.Key{Path: "k", Env: "K", Secret: true}, config.ParseSecret)
	if err = b.Err(); err == nil ||
		!strings.Contains(err.Error(), "k (env K): secret store lookup failed: vault sealed") {
		t.Errorf("err = %v", err)
	}
}

type failingStore struct{}

func (failingStore) LookupSecret(context.Context, string) (secret.Secret, error) {
	return secret.Secret{}, errors.New("vault sealed")
}

func TestSources(t *testing.T) {
	dir := t.TempDir()
	if _, optErr := config.NewSources(config.OptionalFile(filepath.Join(dir, "missing.yaml"))); optErr != nil {
		t.Errorf("a missing optional file is fine: %v", optErr)
	}
	_, err := config.NewSources(
		config.File(filepath.Join(dir, "missing.yaml")),
		config.YAML("bad.yaml", []byte("a: [b: c]")),
	)
	if err == nil || !strings.Contains(err.Error(), "missing.yaml") ||
		!strings.Contains(err.Error(), "lists may hold only plain values") {
		t.Errorf("both source failures must be reported: %v", err)
	}
	store := config.SecretDir(dir)
	if err = os.WriteFile(filepath.Join(dir, "TOKEN"), []byte("abc\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, lookupErr := store.LookupSecret(context.Background(), "TOKEN"); v.Reveal() != "abc" || lookupErr != nil {
		t.Errorf("SecretDir = %v %v", v.IsSet(), lookupErr)
	}
	if v, lookupErr := store.LookupSecret(context.Background(), "ABSENT"); v.IsSet() || lookupErr != nil {
		t.Errorf("absent secret = %v %v", v.IsSet(), lookupErr)
	}
	if _, err = store.LookupSecret(context.Background(), "../etc/passwd"); err == nil {
		t.Error("SecretDir must refuse path traversal")
	}
}

func TestFormat(t *testing.T) {
	v := slog.GroupValue(
		slog.Group("db", slog.String("url", "postgres://x"), slog.Int("max", 3)),
		slog.String("name", "two words"),
		slog.String("empty", ""),
	)
	if got, want := config.Format(v), `db.url=postgres://x db.max=3 name="two words" empty=""`; got != want {
		t.Errorf("Format = %s\nwant     %s", got, want)
	}
}
