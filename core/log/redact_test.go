package log_test

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/log/logtest"
	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

func TestSecretNeverPrints(t *testing.T) {
	t.Parallel()
	s := secret.New("hunter2")
	for _, got := range []string{s.String(), fmt.Sprint(s), fmt.Sprintf("%v %s %#v %+v", s, s, s, s)} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("leaked: %q", got)
		}
	}
	if s.Reveal() != "hunter2" {
		t.Fatal("Reveal lost the value")
	}

	logger, rec := logtest.New()
	logger.InfoContext(t.Context(), "login", slog.Any("credential_ref", s), "user_secret_value", s)
	entry := rec.AssertLogged(t, slog.LevelInfo, "login")
	if strings.Contains(entry.Format(), "hunter2") {
		t.Fatalf("leaked: %s", entry.Format())
	}
}

type apiKey struct {
	ID    string
	Value string
}

func (k apiKey) LogValue() slog.Value {
	// A valuer that forgets to drop the secret: the key guard still catches it.
	return slog.GroupValue(slog.String("id", k.ID), slog.String("token", k.Value))
}

func TestRedactionCoversRecordsWithAttrsGroupsAndValuers(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	logger = logger.With(slog.String("authorization", "Bearer abc"), slog.String("api_key_id", "k1"))

	logger.InfoContext(t.Context(), "call",
		slog.Group("headers", slog.String("Cookie", "sid=1"), slog.String("Accept", "json")),
		slog.Any("key", apiKey{ID: "k1", Value: "sk_live_1"}),
		slog.String("password", "p"),
	)

	entry := rec.AssertLogged(t, slog.LevelInfo, "call",
		slog.String("authorization", secret.Redacted),
		slog.String("api_key_id", "k1"),
		slog.String("headers.Cookie", secret.Redacted),
		slog.String("headers.Accept", "json"),
		slog.String("key.id", "k1"),
		slog.String("key.token", secret.Redacted),
		slog.String("password", secret.Redacted),
	)
	for _, leaked := range []string{"Bearer abc", "sid=1", "sk_live_1"} {
		if strings.Contains(entry.Format(), leaked) {
			t.Errorf("leaked %q: %s", leaked, entry.Format())
		}
	}
}

func TestRedactionCoversContextAttrs(t *testing.T) {
	t.Parallel()
	logger, rec := logtest.New()
	ctx := log.WithAttrs(t.Context(), slog.String("session_token", "abc"))

	logger.InfoContext(ctx, "ctx")

	rec.AssertLogged(t, slog.LevelInfo, "ctx", slog.String("session_token", secret.Redacted))
}

type appConfig struct {
	DSNHost string
	Token   secret.Secret
}

func TestSecretHiddenFromEncoders(t *testing.T) {
	t.Parallel()
	cfg := appConfig{DSNHost: "db.internal", Token: secret.New("STRUCTSECRET")}

	v1, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := jsonv2.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var jsonOut, textOut syncBuffer
	log.New(log.Config{Output: &jsonOut, Format: log.FormatJSON}).
		InfoContext(t.Context(), "config loaded", slog.Any("cfg", cfg))
	log.New(log.Config{Output: &textOut, Format: log.FormatText}).
		InfoContext(t.Context(), "config loaded", slog.Any("cfg", cfg))

	for name, got := range map[string]string{
		"encoding/json": string(v1), "encoding/json/v2": string(v2),
		"JSON handler": jsonOut.String(), "text handler": textOut.String(),
	} {
		if strings.Contains(got, "STRUCTSECRET") || !strings.Contains(got, secret.Redacted) {
			t.Errorf("%s: %s", name, got)
		}
		if !strings.Contains(got, "db.internal") {
			t.Errorf("%s dropped the public field: %s", name, got)
		}
	}
}

// TestUnexportedSecretFieldHidden covers the path where fmt cannot call a
// method: a Secret in an unexported field of a struct logged with slog.Any,
// which the text handler prints by reflection.
func TestUnexportedSecretFieldHidden(t *testing.T) {
	t.Parallel()
	type client struct {
		host  string
		token secret.Secret
	}
	var textOut, jsonOut syncBuffer
	c := client{host: "api.internal", token: secret.New("FIELDSECRET")}
	log.New(log.Config{Output: &textOut, Format: log.FormatText}).
		InfoContext(t.Context(), "client", slog.Any("client", c))
	log.New(log.Config{Output: &jsonOut, Format: log.FormatJSON}).
		InfoContext(t.Context(), "client", slog.Any("client", c))
	for name, got := range map[string]string{
		"text handler": textOut.String(), "JSON handler": jsonOut.String(), "%+v": fmt.Sprintf("%+v", c),
	} {
		if strings.Contains(got, "FIELDSECRET") {
			t.Errorf("%s leaked the value: %s", name, got)
		}
	}
}

// labels is a named map type: it takes the reflection path of the redactor.
type labels map[string]string

func TestRedactionCoversMapsInBothFormats(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{"name": "loop"}
	cyclic["self"] = cyclic
	attrs := []any{
		slog.Any("headers", http.Header{"Authorization": {"Bearer HDRLEAK"}, "Accept": {"application/json"}}),
		slog.Any("form", url.Values{"token": {"FORMLEAK"}, "page": {"2"}}),
		slog.Any("m", map[string]string{"password": "MAPLEAK", "user": "ann"}),
		slog.Any("nested", map[string]any{"db": map[string]any{"dsn": "NESTLEAK", "pool": 4}}),
		slog.Any("labels", labels{"session_token": "NAMEDLEAK", "team": "core"}),
		slog.Any("cyclic", cyclic),
	}
	for _, format := range []log.Format{log.FormatJSON, log.FormatText} {
		var out syncBuffer
		log.New(log.Config{Output: &out, Format: format}).InfoContext(t.Context(), "request", attrs...)
		got := out.String()
		for _, leaked := range []string{"HDRLEAK", "FORMLEAK", "MAPLEAK", "NESTLEAK", "NAMEDLEAK"} {
			if strings.Contains(got, leaked) {
				t.Errorf("format %d leaked %q: %s", format, leaked, got)
			}
		}
		for _, kept := range []string{"application/json", "ann", "core", "loop", "[TRUNCATED]"} {
			if !strings.Contains(got, kept) {
				t.Errorf("format %d lost %q: %s", format, kept, got)
			}
		}
	}

	logger, rec := logtest.New()
	logger.InfoContext(t.Context(), "request", attrs[0])
	rec.AssertLogged(t, slog.LevelInfo, "request",
		slog.String("headers.Authorization", secret.Redacted),
		slog.Any("headers.Accept", []string{"application/json"}),
	)
}

// countingValuer counts its LogValue calls.
type countingValuer struct{ calls *atomic.Int32 }

func (v countingValuer) LogValue() slog.Value {
	v.calls.Add(1)
	return slog.StringValue("lazy")
}

func TestLogValuerResolvedOnce(t *testing.T) {
	t.Parallel()
	tests := map[string]func(slog.Attr) []any{
		"alone":              func(a slog.Attr) []any { return []any{a} },
		"with secret":        func(a slog.Attr) []any { return []any{a, slog.String("password", "p")} },
		"in group":           func(a slog.Attr) []any { return []any{slog.Group("g", a)} },
		"in group by secret": func(a slog.Attr) []any { return []any{slog.Group("g", a, slog.String("token", "t"))} },
	}
	for name, attrs := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var out syncBuffer
			log.New(log.Config{Output: &out}).
				InfoContext(t.Context(), "m", attrs(slog.Any("v", countingValuer{&calls}))...)
			if got := calls.Load(); got != 1 {
				t.Fatalf("LogValue called %d times: %s", got, out.String())
			}
			if !strings.Contains(out.String(), "lazy") {
				t.Fatalf("value lost: %s", out.String())
			}
		})
	}
}
