package example_test

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/internal/example"
)

// The JSON boundary benchmarks compare how a request body of the example API
// (openapi.yaml, CreateWebhookRequest) reaches a handler.
//
//   - impl=legacy-decode is the body decode of oapi-codegen's upstream strict
//     server: json.NewDecoder(r.Body).Decode, with no validation.
//   - impl=legacy is what a v0.22 service runs: the OpenAPI request validator
//     of modules/httpserver (nethttp-middleware over kin-openapi, with the
//     options of openAPIValidator), then the same decode.
//   - impl=beta is nanohttp.Decode, the generated strict server's decode:
//     ReadBody, strict json/v2 and the generated Validate.
//   - impl=beta-read, impl=beta-unmarshal and impl=beta-validate split
//     impl=beta into its three steps.
//
// Both payloads are valid. The 1 KB body uses every member; the 64 KB body
// fills description and the 20 headers the schema allows with 3-byte runes up
// to their maxLength, the largest valid body this schema accepts.

func bench1KB() []byte {
	body := map[string]any{
		"name":        "nightly checkout flow",
		"description": strings.Repeat("Runs the checkout journey against staging. ", 12),
		"url":         "https://hooks.example.com/nanostack/checkout?team=payments",
		"headers": []map[string]string{
			{"name": "X-Team", "value": "payments"},
			{"name": "X-Env", "value": "staging"},
			{"name": "X-Trace", "value": "on"},
		},
		"auth":     map[string]string{"type": "bearer", "token": "tok_0123456789abcdef0123"},
		"run_when": "on_failure",
		"retries":  3,
	}
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return data
}

func bench64KB() []byte {
	headers := make([]map[string]string, 20)
	for i := range headers {
		headers[i] = map[string]string{"name": fmt.Sprintf("X-Header-%02d", i), "value": strings.Repeat("€", 1024)}
	}
	body := map[string]any{
		"name":        "nightly checkout flow",
		"description": strings.Repeat("€", 1000),
		"url":         "https://hooks.example.com/nanostack/checkout?team=payments",
		"headers":     headers,
		"auth":        map[string]string{"type": "bearer", "token": "tok_0123456789abcdef0123"},
		"run_when":    "always",
		"retries":     10,
	}
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return data
}

// legacyValidator builds the request validator as modules/httpserver's
// openAPIValidator does.
func legacyValidator(b *testing.B) func(http.Handler) http.Handler {
	b.Helper()
	spec, err := os.ReadFile("openapi.yaml")
	if err != nil {
		b.Fatal(err)
	}
	swagger, err := openapi3.NewLoader().LoadFromData(spec)
	if err != nil {
		b.Fatal(err)
	}
	return nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			IncludeResponseStatus: true,
			MultiError:            true,
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error {
				return nil
			},
		},
	})
}

type benchResponseWriter struct {
	header http.Header
	status int
}

func (w *benchResponseWriter) Header() http.Header         { return w.header }
func (w *benchResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *benchResponseWriter) WriteHeader(status int)      { w.status = status }

// bodyRequest is one POST /webhooks request whose body is rewound before
// each use.
type bodyRequest struct {
	r       *http.Request
	reader  *bytes.Reader
	payload []byte
}

func newBodyRequest(payload []byte) *bodyRequest {
	r := httptest.NewRequest(http.MethodPost, "/webhooks", http.NoBody)
	r.Header.Set("Content-Type", "application/json")
	r.Host = "api.example.com"
	return &bodyRequest{r: r, reader: bytes.NewReader(payload), payload: payload}
}

func (br *bodyRequest) rewind() *http.Request {
	br.reader.Reset(br.payload)
	br.r.Body = io.NopCloser(br.reader)
	br.r.ContentLength = int64(len(br.payload))
	return br.r
}

func BenchmarkDecode(b *testing.B) {
	for _, size := range []struct {
		name    string
		payload []byte
	}{{"size=1KB", bench1KB()}, {"size=64KB", bench64KB()}} {
		b.Run("impl=legacy-decode/"+size.name, func(b *testing.B) {
			br := newBodyRequest(size.payload)
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				var body example.CreateWebhookRequest
				if err := json.NewDecoder(br.rewind().Body).Decode(&body); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("impl=legacy/"+size.name, func(b *testing.B) {
			decoded := 0
			handler := legacyValidator(b)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body example.CreateWebhookRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				decoded++
				w.WriteHeader(http.StatusCreated)
			}))
			br := newBodyRequest(size.payload)
			w := &benchResponseWriter{header: make(http.Header)}
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				handler.ServeHTTP(w, br.rewind())
			}
			if w.status != http.StatusCreated || decoded == 0 {
				b.Fatalf("status = %d: the validator rejected the body", w.status)
			}
		})

		b.Run("impl=beta/"+size.name, func(b *testing.B) {
			br := newBodyRequest(size.payload)
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				var body example.CreateWebhookRequest
				if err := nanohttp.Decode(br.rewind(), &body); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("impl=beta-read/"+size.name, func(b *testing.B) {
			br := newBodyRequest(size.payload)
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := nanohttp.ReadBody(br.rewind()); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("impl=beta-unmarshal/"+size.name, func(b *testing.B) {
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				var body example.CreateWebhookRequest
				if err := jsonv2.Unmarshal(size.payload, &body, validate.DecodeOptions()); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("impl=beta-validate/"+size.name, func(b *testing.B) {
			var body example.CreateWebhookRequest
			if err := nanohttp.Unmarshal(size.payload, &body); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(size.payload)))
			b.ReportAllocs()
			for b.Loop() {
				// RootOf indexes the document lazily, on the first lookup, so a
				// fresh one per iteration measures the index as Decode pays it.
				if errs := body.Validate(validate.RootOf(size.payload)); len(errs) > 0 {
					b.Fatal(errs)
				}
			}
		})
	}
}

// BenchmarkEncode measures the response side. The generated Visit methods
// are oapi-codegen's upstream template unchanged (encoding/json into a
// buffer, then one write), so legacy and beta run the same code; the
// jsonv2.Marshal rows are a reference for a template that switched.
func BenchmarkEncode(b *testing.B) {
	events := make(example.ListEvents200JSONResponse, 1000)
	for i := range events {
		events[i] = fmt.Sprintf("evt_%06d_delivered", i)
	}
	webhook := example.CreateWebhook201JSONResponse{
		Id: "wh_0123456789", Name: "nightly checkout flow", Url: "https://hooks.example.com/nanostack/checkout",
	}
	w := &benchResponseWriter{header: make(http.Header)}

	b.Run("impl=generated/body=webhook", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := webhook.VisitCreateWebhookResponse(w); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("impl=jsonv2/body=webhook", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := jsonv2.Marshal(webhook)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = w.Write(data)
		}
	})
	b.Run("impl=generated/body=events1000", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := events.VisitListEventsResponse(w); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("impl=jsonv2/body=events1000", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := jsonv2.Marshal(events)
			if err != nil {
				b.Fatal(err)
			}
			_, _ = w.Write(data)
		}
	})
}
