package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nanoconf "github.com/nanostack-dev/nanostack-framework/core/config"
	"github.com/nanostack-dev/nanostack-framework/gen/config/testdata/app"
)

func validEnv() map[string]string {
	return map[string]string{
		"PG_URL":       "postgres://app:hunter2@db:5432/app",
		"BILLING_FROM": "billing@example.com",
		"STRIPE_KEY":   "sk_live_abcdef",
	}
}

func bind(t *testing.T, opts ...nanoconf.Option) (app.Config, error) {
	t.Helper()
	src, err := nanoconf.NewSources(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return app.Bind(context.Background(), src)
}

func TestBindAppliesDefaultsAndParses(t *testing.T) {
	cfg, err := bind(t, nanoconf.EnvMap(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.ReadTimeout != 5*time.Second || cfg.HTTP.Addr != ":8080" || cfg.HTTP.AdminPort != 9090 {
		t.Errorf("http defaults = %+v", cfg.HTTP)
	}
	if cfg.Billing.DueDays != 30 || cfg.Billing.LateFeeRate != 0.015 || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("defaults: due=%d fee=%v level=%v", cfg.Billing.DueDays, cfg.Billing.LateFeeRate, cfg.LogLevel)
	}
	if got := strings.Join(cfg.Billing.Currencies, ","); got != "usd,eur" {
		t.Errorf("currencies = %s", got)
	}
	if cfg.Postgres.URL.Host != "db:5432" || cfg.Billing.StripeKey.Reveal() != "sk_live_abcdef" {
		t.Errorf("url/secret not bound: %v", cfg.Postgres.URL)
	}
	if cfg.Billing.CC != nil || cfg.Billing.WebhookSecret.IsSet() {
		t.Error("optional settings must stay zero when unset")
	}
}

func TestBindReportsEveryIssueWithItsEnvVar(t *testing.T) {
	_, err := bind(t, nanoconf.EnvMap(map[string]string{
		"BILLING_DUE_DAYS":   "0",
		"HTTP_READ_TIMEOUT":  "soon",
		"PG_URL":             "mysql://db/app",
		"BILLING_CURRENCIES": "usd, gbp",
		"ENVIRONMENT":        "qa",
		"LOG_LEVEL":          "loud",
		"HTTP_ADMIN_PORT":    "70000",
	}))
	var cerr *nanoconf.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want *config.Error", err)
	}
	want := []string{
		`environment (env ENVIRONMENT): must be one of: dev, staging, prod; got "qa" from env ENVIRONMENT`,
		`log_level (env LOG_LEVEL): must be a valid slog.Level; got "loud" from env LOG_LEVEL`,
		`http.read_timeout (env HTTP_READ_TIMEOUT): must be a duration such as 300ms, 30s or 5m; got "soon" from env HTTP_READ_TIMEOUT`,
		`http.admin_port (env HTTP_ADMIN_PORT): is out of range; got "70000" from env HTTP_ADMIN_PORT`,
		`postgres.url (env PG_URL): must use scheme postgres or postgresql; got "mysql://db/app" from env PG_URL`,
		`billing.due_days (env BILLING_DUE_DAYS): must be between 1 and 365; got "0" from env BILLING_DUE_DAYS`,
		`billing.from (env BILLING_FROM): is required; set env BILLING_FROM or billing.from in a config file`,
		`billing.stripe_key (env STRIPE_KEY): is required; set env STRIPE_KEY or the secret store entry STRIPE_KEY`,
		`billing.currencies[1] (env BILLING_CURRENCIES): must be one of: usd, eur, cad; got "gbp" from env BILLING_CURRENCIES`,
	}
	if len(cerr.Issues) != len(want) {
		t.Fatalf("got %d issues, want %d:\n%v", len(cerr.Issues), len(want), err)
	}
	for i, w := range want {
		if got := cerr.Issues[i].String(); !strings.HasPrefix(got, w) {
			t.Errorf("issue %d:\n got %s\nwant %s", i, got, w)
		}
	}
	if !strings.HasPrefix(err.Error(), "config: 9 issues: environment (env ENVIRONMENT)") {
		t.Errorf("Error() = %s", err)
	}
}

func TestFileLayersSecretStoreAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "STRIPE_KEY"), []byte("sk_from_store\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []byte("billing:\n  due_days: 45\n  from: base@example.com\n  cc: [a@example.com, b@example.com]\npostgres:\n  url: postgres://db/app\n")
	prod := []byte("billing:\n  due_days: 60\n")
	cfg, err := bind(t,
		nanoconf.EnvMap(map[string]string{"BILLING_FROM": "env@example.com"}),
		nanoconf.YAML("base.yaml", base),
		nanoconf.YAML("prod.yaml", prod),
		nanoconf.Secrets(nanoconf.SecretDir(dir)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Billing.DueDays != 60 {
		t.Errorf("later file layer must win: due_days = %d", cfg.Billing.DueDays)
	}
	if cfg.Billing.From != "env@example.com" {
		t.Errorf("env must beat files: from = %s", cfg.Billing.From)
	}
	if got := strings.Join(cfg.Billing.CC, " "); got != "a@example.com b@example.com" {
		t.Errorf("YAML list: cc = %s", got)
	}
	if cfg.Billing.StripeKey.Reveal() != "sk_from_store" {
		t.Errorf("secret store: got %q", cfg.Billing.StripeKey.Reveal())
	}
}

func TestSecretInFileAndUnknownKeysAreIssues(t *testing.T) {
	env := validEnv()
	_, err := bind(t,
		nanoconf.EnvMap(env),
		nanoconf.YAML("base.yaml", []byte("billing:\n  stripe_key: sk_leaked\n  due_dayz: 3\n")),
	)
	if err == nil {
		t.Fatal("want issues")
	}
	msg := err.Error()
	for _, w := range []string{
		"billing.stripe_key (env STRIPE_KEY): is a secret and must not be in a config file",
		"billing.due_dayz: unknown setting; fix the spelling or remove it; from file base.yaml",
	} {
		if !strings.Contains(msg, w) {
			t.Errorf("missing %q in %s", w, msg)
		}
	}
	if strings.Contains(msg, "sk_leaked") {
		t.Errorf("an issue printed the secret: %s", msg)
	}
}

func TestRedaction(t *testing.T) {
	env := validEnv()
	env["BILLING_WEBHOOK_SECRET"] = "whsec_123456"
	cfg, err := bind(t, nanoconf.EnvMap(env))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("config", "cfg", cfg)
	jsonOut, _ := json.Marshal(cfg)
	outputs := map[string]string{
		"String": cfg.String(),
		"%v":     fmt.Sprintf("%v", cfg),
		"%+v":    fmt.Sprintf("%+v", cfg),
		"%#v":    fmt.Sprintf("%#v", cfg),
		"slog":   logs.String(),
		"json":   string(jsonOut),
	}
	for name, out := range outputs {
		for _, secret := range []string{"sk_live_abcdef", "whsec_123456", "hunter2"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s leaks %q: %s", name, secret, out)
			}
		}
	}
	for _, w := range []string{"billing.stripe_key=[REDACTED]", "postgres.url=postgres://app:[REDACTED]@db:5432/app", "http.read_timeout=5s", "billing.currencies=\"[usd eur]\""} {
		if !strings.Contains(cfg.String(), w) {
			t.Errorf("String() misses %q: %s", w, cfg.String())
		}
	}
	if !strings.Contains(logs.String(), `"stripe_key":"[REDACTED]"`) {
		t.Errorf("slog output: %s", logs.String())
	}
}

// TestQueryPasswordIsRedacted covers a DSN that carries its password in the
// query string, as libpq and most drivers accept.
func TestQueryPasswordIsRedacted(t *testing.T) {
	env := validEnv()
	env["PG_URL"] = "postgres://db/app?sslpassword=hunter2&password=hunter2&sslmode=require"
	cfg, err := bind(t, nanoconf.EnvMap(env))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("config", "cfg", cfg)
	want := `postgres.url="postgres://db/app?sslpassword=[REDACTED]&password=[REDACTED]&sslmode=require"`
	if got := cfg.String(); !strings.Contains(got, want) {
		t.Errorf("String() = %s, want it to contain %s", got, want)
	}
	if strings.Contains(logs.String(), "hunter2") {
		t.Errorf("slog output leaks the password: %s", logs.String())
	}
}

// TestRejectedURLNeverLeaksPassword covers a DSN that fails to parse, whose
// issue shows no value, and one that fails the scheme check, whose issue shows
// it redacted.
func TestRejectedURLNeverLeaksPassword(t *testing.T) {
	for name, url := range map[string]string{
		"no host":    "postgres://app:hunter2@/app",
		"bad port":   "postgres://app:hunter2@db:port/app",
		"bad scheme": "mysql://app:hunter2@db/app",
		"query":      "mysql://db/app?password=hunter2",
		"no scheme":  "app:hunter2@db:5432/app",
		"keywords":   "host=db user=app password=hunter2 dbname=app",
	} {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env["PG_URL"] = url
			_, err := bind(t, nanoconf.EnvMap(env))
			if err == nil || !strings.Contains(err.Error(), "postgres.url (env PG_URL)") {
				t.Fatalf("err = %v, want a postgres.url issue", err)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("issue leaks the password: %v", err)
			}
		})
	}
}
