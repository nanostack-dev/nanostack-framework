package example_test

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

type result struct {
	status int
	ctype  string
	body   string
	p      problem.Problem
}

func do(t *testing.T, h http.Handler, method, target, contentType, body string) result {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	res := result{status: rec.Code, ctype: rec.Header().Get("Content-Type"), body: rec.Body.String()}
	if res.ctype == problem.ContentType {
		if err := jsonv2.Unmarshal(rec.Body.Bytes(), &res.p); err != nil {
			t.Fatalf("decode problem %s: %v", rec.Body.String(), err)
		}
	}
	return res
}

func (r result) errs() []string {
	var out []string
	for _, e := range r.p.Errors {
		loc := e.Pointer
		if e.Parameter != "" {
			loc = "?" + e.Parameter
		}
		out = append(out, loc+" "+e.Code)
	}
	return out
}

const valid = `{"name":"hook","url":"https://example.com/in","headers":[{"name":"X-A","value":"1"}],"auth":{"type":"bearer","token":"0123456789abcdef"},"run_when":"always","retries":3}`

// The 18 must-reject inputs of the ogen trial (reports/ogen-trial.md),
// adapted to this spec, plus the decoding edge cases of nanohttp.Decode.
func TestBoundary(t *testing.T) {
	h, _ := newServer()
	long := strings.Repeat("x", 300)
	cases := []struct {
		name, method, target, ctype, body string
		status                            int
		problemType                       string
		errs                              []string
	}{
		{"valid", "POST", "/webhooks", "application/json", valid, 201, "", nil},
		{"+json media type", "POST", "/webhooks", "application/vnd.api+json", valid, 201, "", nil},
		{"no content type is read as JSON", "POST", "/webhooks", "", valid, 201, "", nil},

		{
			"minLength",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"","url":"https://x.dev"}`,
			422,
			problem.TypeValidation,
			[]string{"/name minLength"},
		},
		{
			"maxLength",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"` + long + `","url":"https://x.dev"}`,
			422,
			problem.TypeValidation,
			[]string{"/name maxLength"},
		},
		{
			"required",
			"POST",
			"/webhooks",
			"application/json",
			`{}`,
			422,
			problem.TypeValidation,
			[]string{"/name required", "/url required"},
		},
		{
			"null on a non-nullable field",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":null,"url":"https://x.dev"}`,
			422,
			problem.TypeValidation,
			[]string{"/name type"},
		},
		{
			"null body is a type error, not missing members",
			"POST",
			"/webhooks",
			"application/json",
			`null`,
			422,
			problem.TypeValidation,
			[]string{" type"},
		},
		{
			"null optional body",
			"PATCH",
			"/webhooks/wh_1",
			"application/json",
			`null`,
			422,
			problem.TypeValidation,
			[]string{" type"},
		},
		{
			"integral number for an integer",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","url":"https://x.dev","retries":3.0}`,
			201,
			"",
			nil,
		},
		{
			"malformed content type",
			"POST",
			"/webhooks",
			"application/json; charset",
			valid,
			400,
			problem.TypeInvalidParameter,
			[]string{"?Content-Type type"},
		},
		{
			"wrong JSON type",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":42,"url":"https://x.dev"}`,
			400,
			problem.TypeInvalidBody,
			[]string{"/name type"},
		},
		{
			"malformed JSON",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":`,
			400,
			problem.TypeMalformedJSON,
			[]string{"/name syntax"},
		},
		{
			"form content type",
			"POST",
			"/webhooks",
			"application/x-www-form-urlencoded",
			"name=a",
			415,
			problem.TypeUnsupportedMediaType,
			nil,
		},
		{
			"query maximum",
			"GET",
			"/webhooks/wh_1/events?limit=101",
			"",
			"",
			400,
			problem.TypeInvalidParameter,
			[]string{"?limit maximum"},
		},
		{
			"query minimum",
			"GET",
			"/webhooks/wh_1/events?limit=0",
			"",
			"",
			400,
			problem.TypeInvalidParameter,
			[]string{"?limit minimum"},
		},
		{
			"query not an integer",
			"GET",
			"/webhooks/wh_1/events?limit=abc",
			"",
			"",
			400,
			problem.TypeInvalidParameter,
			[]string{"?limit type"},
		},
		{
			"path pattern",
			"GET",
			"/webhooks/-x/events",
			"",
			"",
			400,
			problem.TypeInvalidParameter,
			[]string{"?id pattern"},
		},
		{
			"path pattern on another operation",
			"GET",
			"/webhooks/WH_1",
			"",
			"",
			400,
			problem.TypeInvalidParameter,
			[]string{"?id pattern"},
		},
		{
			"unknown discriminator",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","url":"https://x.dev","auth":{"type":"teleport"}}`,
			422,
			problem.TypeValidation,
			[]string{"/auth/type discriminator"},
		},
		{
			"required inside the selected variant keeps its path",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","url":"https://x.dev","auth":{"type":"basic","username":"u"}}`,
			422,
			problem.TypeValidation,
			[]string{"/auth/password required"},
		},
		{
			"enum",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","url":"https://x.dev","run_when":"sometimes"}`,
			422,
			problem.TypeValidation,
			[]string{"/run_when enum"},
		},
		{
			"every violation at once",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"","description":"` + strings.Repeat(
				"d",
				1001,
			) + `","url":"nope","headers":[{"name":"bad name","value":"v","extra":1}],"retries":11}`,
			422,
			problem.TypeValidation,
			[]string{
				"/name minLength",
				"/description maxLength",
				"/url format",
				"/headers/0/name pattern",
				"/headers/0/extra additionalProperties",
				"/retries maximum",
			},
		},

		{
			"duplicate member names",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","name":"b","url":"https://x.dev"}`,
			400,
			problem.TypeMalformedJSON,
			[]string{"/name duplicate_name"},
		},
		{
			"invalid UTF-8",
			"POST",
			"/webhooks",
			"application/json",
			"{\"name\":\"\xff\",\"url\":\"https://x.dev\"}",
			400,
			problem.TypeMalformedJSON,
			[]string{"/name invalid_utf8"},
		},
		{
			"trailing data",
			"POST",
			"/webhooks",
			"application/json",
			valid + ` {}`,
			400,
			problem.TypeMalformedJSON,
			[]string{" trailing_data"},
		},
		{
			"names match case-sensitively",
			"POST",
			"/webhooks",
			"application/json",
			`{"NAME":"a","url":"https://x.dev"}`,
			422,
			problem.TypeValidation,
			[]string{"/name required"},
		},
		{
			"int32 overflow",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"a","url":"https://x.dev","retries":3000000000}`,
			400,
			problem.TypeInvalidBody,
			[]string{"/retries format"},
		},
		{"empty required body", "POST", "/webhooks", "application/json", "", 400, problem.TypeMissingBody, nil},
		{"blank required body", "POST", "/webhooks", "application/json", "  \n", 400, problem.TypeMissingBody, nil},
		{
			"body over the limit",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"` + strings.Repeat("x", 5000) + `"}`,
			413,
			problem.TypeBodyTooLarge,
			nil,
		},

		{
			"handler Described error",
			"GET",
			"/webhooks/wh_2",
			"",
			"",
			404,
			"https://errors.nanostack.dev/webhooks/webhook_not_found",
			nil,
		},
		{
			"handler plain error is a safe 500",
			"POST",
			"/webhooks",
			"application/json",
			`{"name":"boom","url":"https://x.dev"}`,
			500,
			problem.TypeInternal,
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := do(t, h, c.method, c.target, c.ctype, c.body)
			if r.status != c.status {
				t.Fatalf("status = %d, want %d: %s", r.status, c.status, r.body)
			}
			if c.problemType == "" {
				return
			}
			if r.ctype != problem.ContentType || r.p.Type != c.problemType {
				t.Fatalf("problem = %q %+v, want type %s", r.ctype, r.p, c.problemType)
			}
			if got := r.errs(); fmt.Sprint(got) != fmt.Sprint(c.errs) {
				t.Errorf("errors = %q\nwant     %q", got, c.errs)
			}
			for _, leak := range []string{"10.0.0.3", "shard 7", "pq:", "jsontext", "json:"} {
				if strings.Contains(r.body, leak) {
					t.Errorf("response leaks %q: %s", leak, r.body)
				}
			}
			if r.p.Instance != strings.Split(c.target, "?")[0] {
				t.Errorf("instance = %q", r.p.Instance)
			}
		})
	}
}

func TestDescribedDetailIsSent(t *testing.T) {
	h, _ := newServer()
	r := do(t, h, "GET", "/webhooks/wh_2", "", "")
	if r.p.Detail != "No webhook has id wh_2." || r.p.Title != "Webhook not found" {
		t.Fatalf("problem = %+v", r.p)
	}
}
