package oapitemplates_test

import (
	"os"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/oapitemplates"
)

func TestUserTemplates(t *testing.T) {
	tmpl := oapitemplates.UserTemplates()["strict/strict-http.tmpl"]
	onDisk, err := os.ReadFile("strict/strict-http.tmpl")
	if err != nil || tmpl != string(onDisk) {
		t.Fatalf("embedded template differs from the file: %v", err)
	}
	for _, want := range []string{"nanohttp.Decode(r, &body)", "request.validateParams()", "nanohttp.RequestError", "func NewHTTPHandler("} {
		if !strings.Contains(tmpl, want) {
			t.Errorf("template lacks %q", want)
		}
	}
	if strings.Contains(tmpl, "err.Error()") {
		t.Error("the override must never send err.Error() to a client")
	}
}

func TestUnionTemplateDecodesStrictly(t *testing.T) {
	tmpl := oapitemplates.UserTemplates()["union.tmpl"]
	onDisk, err := os.ReadFile("union.tmpl")
	if err != nil || tmpl != string(onDisk) {
		t.Fatalf("embedded template differs from the file: %v", err)
	}
	for _, want := range []string{
		"err := nanohttp.Unmarshal(t.union, &body)",
		"err := nanohttp.Unmarshal(t.union, &discriminator)",
		"err = nanohttp.Unmarshal(raw, &t.{{.GoFieldName}})",
	} {
		if !strings.Contains(tmpl, want) {
			t.Errorf("template lacks %q", want)
		}
	}
	if strings.Contains(tmpl, "json.Unmarshal(t.union") {
		t.Error("a union decoder still uses encoding/json v1")
	}
}
