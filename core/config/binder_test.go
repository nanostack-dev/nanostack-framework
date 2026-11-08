package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config"
)

func issues(t *testing.T, err error) []config.Issue {
	t.Helper()
	if err == nil {
		return nil
	}
	var cerr *config.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want *config.Error", err)
	}
	return cerr.Issues
}

// TestRejectedValueNeverLeaksPassword covers DSNs that fail to parse or fail a
// check. A URL setting shows no value it cannot parse; a setting of another
// type shows the value with every credential redacted; a URL that parses but
// fails a check is shown through secret.RedactURL.
func TestRejectedValueNeverLeaksPassword(t *testing.T) {
	const pw = "Pw9SECRET"
	malformed := []string{
		"postgres://app:" + pw + "@/app",
		"postgres://app:" + pw + "@db:port/app",
		"app:" + pw + "@db:5432/app",
		"postgres:/app:" + pw + "@db/app",
		"postgres://app:my " + pw + "@db/app",
		"postgres://app:" + pw + "\n@db/app",
		"host=db user=app password=" + pw + " dbname=app",
		// No "@" and no key: only a URL setting can hide these, by showing
		// nothing. A setting of another type shows them.
		"postgres://app:" + pw,
		pw,
	}
	for i, raw := range malformed {
		port := raw
		if i >= len(malformed)-2 {
			port = "not a number"
		}
		src, err := config.NewSources(config.EnvMap(map[string]string{
			"PG_URL": raw,
			"HOSTS":  "https://ok.example.com," + raw,
			"PORT":   port,
		}))
		if err != nil {
			t.Fatal(err)
		}
		b := config.NewBinder(context.Background(), src)
		config.Scalar(b, config.Key{Path: "postgres.url", Env: "PG_URL"}, config.ParseURL)
		config.List(b, config.Key{Path: "hosts", Env: "HOSTS"}, config.ParseURL)
		config.Scalar(b, config.Key{Path: "port", Env: "PORT"}, config.ParseInt[int])
		err = b.Err()
		ps := issues(t, err)
		if len(ps) != 3 {
			t.Fatalf("%q: got %d issues, want 3: %v", raw, len(ps), err)
		}
		if strings.Contains(err.Error(), pw) {
			t.Errorf("%q: Error() leaks the password: %v", raw, err)
		}
		if ps[0].Value != "" || ps[1].Value != "" {
			t.Errorf("%q: URL issues show %q and %q, want no value", raw, ps[0].Value, ps[1].Value)
		}
		if ps[1].Path != "hosts[1]" {
			t.Errorf("list issue path = %q, want hosts[1]", ps[1].Path)
		}
	}

	src, err := config.NewSources(config.EnvMap(map[string]string{
		"PG_URL": "mysql://app:" + pw + "@db/app?password=" + pw + "&sslmode=disable",
	}))
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(context.Background(), src)
	u, _ := config.Scalar(b, config.Key{Path: "postgres.url", Env: "PG_URL"}, config.ParseURL)
	config.Scheme(b, config.Key{Path: "postgres.url", Env: "PG_URL"}, u, "postgres", "postgresql")
	ps := issues(t, b.Err())
	want := "mysql://app:[REDACTED]@db/app?password=[REDACTED]&sslmode=disable"
	if len(ps) != 1 || ps[0].Value != want {
		t.Fatalf("scheme issues = %+v, want one with value %q", ps, want)
	}
}

// TestTextSettingNeverLeaksPassword binds DSNs to ParseText settings, whose
// UnmarshalText errors repeat the raw value. No printing path of the issue,
// the error or its JSON may show the password.
func TestTextSettingNeverLeaksPassword(t *testing.T) {
	const pw = "hunter2PW"
	dsns := []string{
		"postgres://app:" + pw + "@db:5432/app",
		"app:" + pw + "@db",
		"host=db password=" + pw,
		`host=db password="a ` + pw + `"`,
		"postgres://db/app?password=" + pw,
		"Server=db;Password=" + pw + ";",
		"jdbc:postgresql://db/app?user=app&password=" + pw,
		"pass=" + pw,
	}
	for _, raw := range dsns {
		src, err := config.NewSources(config.EnvMap(map[string]string{"LOG_LEVEL": raw, "ADDR": raw}))
		if err != nil {
			t.Fatal(err)
		}
		b := config.NewBinder(context.Background(), src)
		config.Scalar(b, config.Key{Path: "log_level", Env: "LOG_LEVEL"}, config.ParseText[slog.Level])
		config.Scalar(b, config.Key{Path: "addr", Env: "ADDR"}, config.ParseText[netip.Addr])
		err = b.Err()
		ps := issues(t, err)
		if len(ps) != 2 {
			t.Fatalf("%q: got %d issues, want 2: %v", raw, len(ps), err)
		}
		if ps[0].Message != "must be a valid slog.Level" || ps[1].Message != "must be a valid netip.Addr" {
			t.Errorf("%q: messages = %q, %q", raw, ps[0].Message, ps[1].Message)
		}
		js, jsonErr := json.Marshal(err)
		if jsonErr != nil {
			t.Fatal(jsonErr)
		}
		for name, out := range map[string]string{
			"Error()": err.Error(),
			"%v":      fmt.Sprintf("%v", err),
			"%+v":     fmt.Sprintf("%+v", err),
			"%#v":     fmt.Sprintf("%#v", err),
			"json":    string(js),
		} {
			if strings.Contains(out, pw) {
				t.Errorf("%q: %s leaks the password: %s", raw, name, out)
			}
		}
	}
}

// TestEmptyListCountsAsUnset covers env "," and YAML []: no elements means
// unset, so a required list reports an issue and a default applies.
func TestEmptyListCountsAsUnset(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		yaml string
	}{
		"env comma":        {env: map[string]string{"HOSTS": ","}},
		"env spaced comma": {env: map[string]string{"HOSTS": " , "}},
		"yaml empty list":  {yaml: "hosts: []\n"},
		"yaml comma":       {yaml: "hosts: \",\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			opts := []config.Option{config.EnvMap(tc.env)}
			if tc.yaml != "" {
				opts = append(opts, config.YAML("base.yaml", []byte(tc.yaml)))
			}
			src, err := config.NewSources(opts...)
			if err != nil {
				t.Fatal(err)
			}

			b := config.NewBinder(context.Background(), src)
			v, ok := config.List(b, config.Key{Path: "hosts", Env: "HOSTS"}, config.ParseString[string])
			ps := issues(t, b.Err())
			if ok || v != nil || len(ps) != 1 ||
				ps[0].String() != "hosts (env HOSTS): is required; set env HOSTS or hosts in a config file" {
				t.Errorf("required: got %v %v, issues %v", v, ok, ps)
			}

			b = config.NewBinder(context.Background(), src)
			v, ok = config.List(b, config.Key{Path: "hosts", Env: "HOSTS", Default: "a,b"}, config.ParseString[string])
			if !ok || strings.Join(v, "|") != "a|b" || b.Err() != nil {
				t.Errorf("default: got %v %v, err %v", v, ok, b.Err())
			}

			b = config.NewBinder(context.Background(), src)
			v, ok = config.List(b, config.Key{Path: "hosts", Env: "HOSTS", Optional: true}, config.ParseString[string])
			if ok || v != nil || b.Err() != nil {
				t.Errorf("optional: got %v %v, err %v", v, ok, b.Err())
			}
		})
	}
}

// TestEmptyListFallsThroughToLowerLayer: an empty value is unset, so the next
// source in precedence order is used, as for scalars.
func TestEmptyListFallsThroughToLowerLayer(t *testing.T) {
	src, err := config.NewSources(
		config.EnvMap(map[string]string{"HOSTS": ","}),
		config.YAML("base.yaml", []byte("hosts: [a, b]\n")),
		config.YAML("dev.yaml", []byte("hosts: []\n")),
	)
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(context.Background(), src)
	v, ok := config.List(b, config.Key{Path: "hosts", Env: "HOSTS"}, config.ParseString[string])
	if !ok || strings.Join(v, "|") != "a|b" || b.Err() != nil {
		t.Errorf("got %v %v, err %v", v, ok, b.Err())
	}
}

func TestErrIsRepeatable(t *testing.T) {
	src, err := config.NewSources(config.YAML("base.yaml", []byte("typo: 1\n")))
	if err != nil {
		t.Fatal(err)
	}
	b := config.NewBinder(context.Background(), src)
	first, second := issues(t, b.Err()), issues(t, b.Err())
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("Err() twice: %v then %v, want one unknown-key issue each", first, second)
	}
}

func TestProfile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("base.yaml", "name: base\nport: 80\n")
	write("dev.yaml", "name: dev\n")

	bind := func(src *config.Sources) (string, int) {
		b := config.NewBinder(context.Background(), src)
		name, _ := config.Scalar(b, config.Key{Path: "name", Env: "NAME"}, config.ParseString[string])
		port, _ := config.Scalar(b, config.Key{Path: "port", Env: "PORT"}, config.ParseInt[int])
		if err := b.Err(); err != nil {
			t.Fatal(err)
		}
		return name, port
	}

	src, err := config.NewSources(config.Profile(dir, "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if name, port := bind(src); name != "dev" || port != 80 {
		t.Errorf("profile dev: name=%s port=%d, want dev over base", name, port)
	}
	if src, err = config.NewSources(config.Profile(dir, "")); err != nil {
		t.Fatal(err)
	}
	if name, _ := bind(src); name != "base" {
		t.Errorf("no profile: name=%s, want base", name)
	}
	t.Setenv(config.ProfileEnv, "dev")
	if src, err = config.NewSources(config.ProfileFromEnv(dir)); err != nil {
		t.Fatal(err)
	}
	if name, _ := bind(src); name != "dev" {
		t.Errorf("NANO_PROFILE=dev: name=%s", name)
	}
	if _, err = config.NewSources(
		config.Profile(dir, "prdo"),
	); err == nil ||
		!strings.Contains(err.Error(), "prdo.yaml") {
		t.Errorf("a misspelled profile must fail: %v", err)
	}
	for _, bad := range []string{"../etc", "base"} {
		if _, err = config.NewSources(config.Profile(dir, bad)); err == nil {
			t.Errorf("profile %q must be refused", bad)
		}
	}
}
