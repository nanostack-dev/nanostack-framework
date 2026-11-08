package secret_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

const raw = "sk_live_123"

type holder struct {
	Public string
	Key    secret.Secret
	hidden secret.Secret
}

func TestSecretNeverPrints(t *testing.T) {
	t.Parallel()
	s := secret.New(raw)
	h := holder{Public: "visible", Key: s, hidden: s}
	v1, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := jsonv2.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	text, err := s.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("x", slog.Any("key", s), slog.Any("holder", h))
	slog.New(slog.NewTextHandler(&logs, nil)).Info("x", "key", s, "holder", h)

	outputs := map[string]string{
		"String": s.String(), "GoString": s.GoString(), "LogValue": s.LogValue().String(),
		"MarshalText": string(text), "json v1": string(v1), "json v2": string(v2), "slog": logs.String(),
		"%v": fmt.Sprintf("%v", s), "%+v": fmt.Sprintf("%+v", s), "%#v": fmt.Sprintf("%#v", s),
		"%s": fmt.Sprintf("%s", s), "%q": fmt.Sprintf("%q", s), "%x": fmt.Sprintf("%x", s),
		"%v struct": fmt.Sprintf("%v", h), "%+v struct": fmt.Sprintf("%+v", h), "%#v struct": fmt.Sprintf("%#v", h),
		"%v ptr": fmt.Sprintf("%v", &h), "%+v ptr": fmt.Sprintf("%+v", &h),
	}
	rawHex := hex.EncodeToString([]byte(raw))
	for name, got := range outputs {
		if strings.Contains(got, raw) || strings.Contains(strings.ToLower(got), rawHex) {
			t.Errorf("%s leaked the value: %s", name, got)
		}
	}
	for _, name := range []string{"String", "MarshalText", "json v1", "json v2", "slog", "%v", "%q"} {
		if !strings.Contains(outputs[name], secret.Redacted) {
			t.Errorf("%s does not show %s: %s", name, secret.Redacted, outputs[name])
		}
	}
	if s.Reveal() != raw || !s.IsSet() {
		t.Fatal("Reveal lost the value")
	}
}

func TestUnsetSecretIsEmpty(t *testing.T) {
	t.Parallel()
	for _, s := range []secret.Secret{{}, secret.New("")} {
		if s.IsSet() || s.Reveal() != "" || s.String() != "" || fmt.Sprint(s) != "" {
			t.Errorf("unset secret prints %q", fmt.Sprint(s))
		}
	}
}

func TestSecretDecodesFromText(t *testing.T) {
	t.Parallel()
	var v1, v2 struct{ Key secret.Secret }
	if err := json.Unmarshal([]byte(`{"Key":"`+raw+`"}`), &v1); err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal([]byte(`{"Key":"`+raw+`"}`), &v2); err != nil {
		t.Fatal(err)
	}
	var s secret.Secret
	if err := s.UnmarshalText([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]secret.Secret{"json v1": v1.Key, "json v2": v2.Key, "text": s} {
		if got.Reveal() != raw {
			t.Errorf("%s decoded %q", name, got.Reveal())
		}
	}
}
