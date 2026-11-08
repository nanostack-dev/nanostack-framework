package example

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
)

// TestUnionReadsWhatWasValidated sends a member whose name differs only in
// case next to the validated one. The handler must read the validated value:
// encoding/json v1 would read "TOKEN" into Token and skip minLength.
func TestUnionReadsWhatWasValidated(t *testing.T) {
	body := []byte(`{"name":"n","url":"https://x.test",` +
		`"auth":{"type":"bearer","token":"0123456789abcdef","TOKEN":"x"}}`)
	var req CreateWebhookRequest
	if err := nanohttp.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if errs := req.Validate(validate.RootOf(body)); len(errs) > 0 {
		t.Fatalf("Validate = %v, want none", errs)
	}
	bearer, err := req.Auth.AsBearerAuth()
	if err != nil {
		t.Fatal(err)
	}
	if bearer.Token != "0123456789abcdef" {
		t.Fatalf("handler reads token %q, want the validated value", bearer.Token)
	}
	kind, err := req.Auth.Discriminator()
	if err != nil || kind != "bearer" {
		t.Fatalf("Discriminator = %q, %v", kind, err)
	}
}
