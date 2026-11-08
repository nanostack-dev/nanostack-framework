package golden_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/unions"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
)

// TestUnionMembersDecodeStrictly covers the union template: a union member
// decodes with nanohttp.Unmarshal, as the body does, so a case variant of a
// member key ("TOKEN") cannot replace the value that validation checked.
// With encoding/json v1, AsBearer matched "TOKEN" case-insensitively and
// returned "x".
func TestUnionMembersDecodeStrictly(t *testing.T) {
	var body unions.Body
	raw := []byte(`{"auth":{"type":"bearer","token":"0123456789","TOKEN":"x"}}`)
	if err := nanohttp.Unmarshal(raw, &body); err != nil {
		return // rejecting the whole body is safe too
	}
	bearer, err := body.Auth.AsBearer()
	if err == nil && bearer.Token != "0123456789" {
		t.Fatalf("AsBearer token = %q, want the validated value or an error", bearer.Token)
	}
}
