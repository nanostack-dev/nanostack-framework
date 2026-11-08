package errorenvelope_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/errorenvelope"
)

// webhook validates as the generated validator of
// http/nanohttp/internal/example validates its CreateWebhookRequest headers
// (that package is internal to http/nanohttp, and http may not import this
// legacy package): it walks the headers until the accumulator is Full, and
// each header allows only name and value.
type webhook struct {
	Headers []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
}

func (w *webhook) Validate(p validate.Path) validate.Errors {
	var errs validate.Errors
	headers := p.Field("headers")
	for i := range w.Headers {
		if errs.Full() {
			break
		}
		h := headers.Index(i)
		errs.Required(h.Field("name"), true)
		errs.Required(h.Field("value"), true)
		errs.OnlyMembers(h, "name", "value")
	}
	return errs
}

// The hostile body of http/nanohttp/internal/example (one header with many
// unknown members, within the default 1 MiB limit) on the envelope path: a
// handler whose request hook is the Writer's RequestError. The envelope
// lists at most validate.MaxErrors details, and the allocation stays
// bounded. The allocation limit sits between the capped and the uncapped
// validate.Errors (34x and 62x the body, measured on an M2 Max); the details
// alone cannot show the cap, because the problem lists at most
// validate.MaxErrors either way.
func TestHostileBodyOnTheEnvelopePathIsBounded(t *testing.T) {
	var members strings.Builder
	members.WriteString(`{"headers":[{"name":"a","value":"b"`)
	for i := 0; members.Len() < 1<<20-40; i++ {
		fmt.Fprintf(&members, `,"m%d":0`, i)
	}
	members.WriteString(`}]}`)
	body := members.String()
	envelope := errorenvelope.New(errorenvelope.Options{Logger: slog.New(slog.DiscardHandler)})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := nanohttp.Decode(r, &webhook{}); err != nil {
			envelope.RequestError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	h.ServeHTTP(rec, r)
	runtime.ReadMemStats(&after)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %.300s", rec.Code, rec.Body.String())
	}
	if n := strings.Count(rec.Body.String(), `"code":"BAD_REQUEST"`); n != validate.MaxErrors {
		t.Fatalf("the envelope lists %d details, want %d", n, validate.MaxErrors)
	}
	if rec.Body.Len() > 32<<10 {
		t.Fatalf("a %d-byte request got a %d-byte envelope", len(body), rec.Body.Len())
	}
	got, limit := after.TotalAlloc-before.TotalAlloc, uint64(48*len(body))
	t.Logf("allocated %.1fx the body", float64(got)/float64(len(body)))
	if got > limit {
		t.Fatalf("a %d-byte request allocated %d bytes, more than %d", len(body), got, limit)
	}
}
