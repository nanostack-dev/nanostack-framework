package example_test

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/internal/example"
)

// A long unknown member over a wide array, within the default 1 MiB limit:
// json/v2 skips the member and the document index stays linear in the body.
// The pointer-keyed index allocated 788 MB here.
func TestWideUnknownMemberStaysLinear(t *testing.T) {
	_, s := newServer()
	h := example.NewHTTPHandler(s, example.HTTPHandlerOptions{}) // the default limit
	var b strings.Builder
	b.WriteString(`{"name":"x","url":"https://a.b","` + strings.Repeat("k", 8000) + `":[0`)
	b.WriteString(strings.Repeat(",0", 99_999))
	b.WriteString(`]}`)
	body := b.String()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	r := do(t, h, "POST", "/webhooks", "application/json", body)
	runtime.ReadMemStats(&after)

	if r.status != http.StatusCreated {
		t.Fatalf("status = %d: %s", r.status, r.body)
	}
	if got, limit := after.TotalAlloc-before.TotalAlloc, uint64(16*len(body)); got > limit {
		t.Fatalf("a %d-byte request allocated %d bytes, more than %d", len(body), got, limit)
	}
}

func TestOptionalBody(t *testing.T) {
	h, s := newServer()
	if r := do(t, h, "PATCH", "/webhooks/wh_1", "", ""); r.status != 200 {
		t.Fatalf("empty optional body: %d %s", r.status, r.body)
	}
	if got := <-s.gotBody; got != nil {
		t.Fatalf("absent optional body must stay nil, got %+v", got)
	}
	if r := do(t, h, "PATCH", "/webhooks/wh_1", "application/json", `{"description":null}`); r.status != 200 {
		t.Fatalf("null nullable member: %d %s", r.status, r.body)
	}
	if got := <-s.gotBody; got == nil || got.Description != nil {
		t.Fatalf("body = %+v", got)
	}
	r := do(t, h, "PATCH", "/webhooks/wh_1", "application/json", `{}`)
	if r.status != 422 || fmt.Sprint(r.errs()) != "[ minProperties]" {
		t.Fatalf("minProperties on an optional body: %d %v", r.status, r.errs())
	}
}
