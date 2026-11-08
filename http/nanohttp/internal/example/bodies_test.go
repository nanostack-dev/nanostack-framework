package example_test

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
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
	got, limit := after.TotalAlloc-before.TotalAlloc, uint64(16*len(body))
	t.Logf("allocated %.1fx the body", float64(got)/float64(len(body)))
	if got > limit {
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

// hostileBodies are two bodies within the default 1 MiB limit: a capped
// array of empty objects, whose items miss their required members, and one
// header with many unknown members. Without validate.MaxErrors each would
// give one violation per item or member.
func hostileBodies() map[string]string {
	const size = 1 << 20
	emptyItems := `{"name":"a","url":"https://x.dev","headers":[{}` +
		strings.Repeat(",{}", (size-60)/3) + `]}`
	var members strings.Builder
	members.WriteString(`{"name":"a","url":"https://x.dev","headers":[{"name":"a","value":"b"`)
	for i := 0; members.Len() < size-40; i++ {
		fmt.Fprintf(&members, `,"m%d":0`, i)
	}
	members.WriteString(`}]}`)
	return map[string]string{"empty items": emptyItems, "unknown members": members.String()}
}

// The generated Validate keeps at most validate.MaxErrors+1 violations of a
// hostile document: it stops at validate.MaxErrors, and the accumulator
// drops the rest. The response lists only the first validate.MaxErrors
// whatever the accumulator holds, so only this count shows the cap.
func TestHostileBodyValidateKeepsMaxErrorsPlusOne(t *testing.T) {
	for name, body := range hostileBodies() {
		t.Run(name, func(t *testing.T) {
			var req example.CreateWebhookRequest
			if err := nanohttp.Unmarshal([]byte(body), &req); err != nil {
				t.Fatal(err)
			}
			errs := req.Validate(validate.RootOf([]byte(body)))
			if len(errs) == 0 || len(errs) > validate.MaxErrors+1 {
				t.Fatalf("Validate kept %d violations, want 1 to %d", len(errs), validate.MaxErrors+1)
			}
		})
	}
	var req example.CreateWebhookRequest
	body := hostileBodies()["unknown members"]
	if err := nanohttp.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if errs := req.Validate(validate.RootOf([]byte(body))); !errs.Truncated() {
		t.Fatalf("Validate kept %d violations of tens of thousands, want a truncated list", len(errs))
	}
}

// A hostile body gets a bounded response and a bounded allocation. Before
// the cap, the array body produced a 53 MB response and 917 MB of
// allocation. The allocation limits sit between the capped and the uncapped
// validator (measured on an M2 Max): the response alone cannot show the cap,
// because it lists at most validate.MaxErrors violations either way.
func TestHostileBodyHasABoundedResponse(t *testing.T) {
	_, s := newServer()
	h := example.NewHTTPHandler(s, example.HTTPHandlerOptions{}) // the default limit
	limits := map[string]int{
		// About 57x the body, most of it the json/v2 decode of the items into
		// Go values. The items exceed maxItems, so the cap does not apply.
		"empty items": 128,
		// About 34x the body with the cap; 62x without it, when the
		// validator records one violation for each of about 96,000 members.
		"unknown members": 48,
	}

	for name, body := range hostileBodies() {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			r := do(t, h, "POST", "/webhooks", "application/json", body)
			runtime.ReadMemStats(&after)

			if r.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d: %.300s", r.status, r.body)
			}
			if n := len(r.p.Errors); n > validate.MaxErrors {
				t.Fatalf("the problem lists %d violations, more than %d", n, validate.MaxErrors)
			}
			if len(r.body) > 32<<10 {
				t.Fatalf("a %d-byte request got a %d-byte response", len(body), len(r.body))
			}
			got, limit := after.TotalAlloc-before.TotalAlloc, uint64(limits[name]*len(body))
			t.Logf("allocated %.1fx the body", float64(got)/float64(len(body)))
			if got > limit {
				t.Fatalf("a %d-byte request allocated %d bytes, more than %d", len(body), got, limit)
			}
		})
	}
}

// A text/plain body is read within the same limit as a JSON body: the
// framework's default bounds it when no MaxBodyBytes middleware does.
func TestTextBodyHasTheDefaultLimit(t *testing.T) {
	_, s := newServer()
	h := example.NewHTTPHandler(s, example.HTTPHandlerOptions{}) // the default limit
	if r := do(t, h, "POST", "/notes", "text/plain", "hello"); r.status != http.StatusNoContent {
		t.Fatalf("a small note: status %d: %s", r.status, r.body)
	}
	r := do(t, h, "POST", "/notes", "text/plain", strings.Repeat("a", int(nanohttp.DefaultMaxBodyBytes)+1))
	if r.status != http.StatusRequestEntityTooLarge || r.p.Code != "http.body_too_large" {
		t.Fatalf("an oversized note: status %d, problem %+v", r.status, r.p)
	}
}
