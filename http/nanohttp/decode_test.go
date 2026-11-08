package nanohttp_test

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

type body struct {
	Name string `json:"name"`
	seen validate.Path
}

func (b *body) Validate(p validate.Path) validate.Errors {
	b.seen = p
	var errs validate.Errors
	errs.Required(p.Field("name"), true)
	return errs
}

func TestDecodeAttachesTheDocument(t *testing.T) {
	var b body
	err := nanohttp.Decode(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)), &b)
	if !b.seen.HasDocument() {
		t.Fatal("Validate must receive a path with the document")
	}
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.ProblemStatus() != 422 || pe.FieldErrors()[0].Pointer != "/name" {
		t.Fatalf("err = %v", err)
	}
	present := nanohttp.Decode(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":""}`)), &b)
	if present != nil {
		t.Fatalf("present empty name: %v", present)
	}
}

func TestDecodeEmptyBody(t *testing.T) {
	for _, r := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/", nil),
		httptest.NewRequest(http.MethodPost, "/", strings.NewReader(" \t\n")),
	} {
		err := nanohttp.Decode(r, &body{})
		if !errors.Is(err, nanohttp.ErrNoBody) || problem.FromError(err).Type != problem.TypeMissingBody {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestDecodeLimit(t *testing.T) {
	var got error
	h := nanohttp.MaxBodyBytes(8)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = nanohttp.Decode(r, &body{})
	}))
	h.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"0123456789"}`)),
	)
	if p := problem.FromError(got); p.Status != 413 {
		t.Fatalf("problem = %+v (%v)", p, got)
	}
	var mb *http.MaxBytesError
	if !errors.As(got, &mb) || mb.Limit != 8 {
		t.Fatalf("the MaxBytesError must stay in the chain: %v", got)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestDecodeReadFailure(t *testing.T) {
	err := nanohttp.Decode(httptest.NewRequest(http.MethodPost, "/", failingReader{}), &body{})
	if p := problem.FromError(err); p.Status != 400 {
		t.Fatalf("problem = %+v", p)
	}
}

func TestRequestErrorNeverEchoesUnknownErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	nanohttp.RequestError(
		rec,
		httptest.NewRequest(http.MethodPost, "/", nil),
		errors.New("multipart: secret boundary detail"),
	)
	if rec.Code != 400 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var p problem.Problem
	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Type != problem.TypeBadRequest || p.Code != "http.bad_request" ||
		p.Detail != "The request could not be read." {
		t.Fatalf("problem = %+v, want problem.BadRequest", p)
	}
}

func TestBadRequestKeepsTheCause(t *testing.T) {
	cause := errors.New("multipart: NextPart: EOF")
	err := problem.BadRequest(cause)
	if !errors.Is(err, cause) {
		t.Fatalf("BadRequest(%v) does not wrap its cause", cause)
	}
	if p := problem.FromError(err); p.Status != http.StatusBadRequest || strings.Contains(p.Detail, "multipart") {
		t.Fatalf("problem = %+v", p)
	}
}

func TestDecodeContentType(t *testing.T) {
	cases := []struct {
		ctype      string
		status     int
		detail     string
		notEchoed  string
		paramError bool
	}{
		{"application/json; charset=utf-8", 0, "", "", false},
		{"application/merge-patch+json", 0, "", "", false},
		{"Application/JSON", 0, "", "", false},
		// A header that does not parse is a malformed request, not a media
		// type the server refuses.
		{"application/json; charset", 400, "", "charset", true},
		{"application/json; =x", 400, "", "=x", true},
		{"text/plain; secret=1", 415, `The request body must be application/json, not "text/plain".`, "secret", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"a"}`))
		r.Header.Set("Content-Type", c.ctype)
		err := nanohttp.Decode(r, &body{})
		if c.status == 0 {
			if err != nil {
				t.Errorf("%q: %v", c.ctype, err)
			}
			continue
		}
		p := problem.FromError(err)
		if p.Status != c.status || c.detail != "" && p.Detail != c.detail {
			t.Errorf("%q: problem = %+v", c.ctype, p)
		}
		if c.paramError && (p.Type != problem.TypeInvalidParameter || len(p.Errors) != 1 ||
			p.Errors[0].Parameter != "Content-Type") {
			t.Errorf("%q: problem = %+v", c.ctype, p)
		}
		rec := httptest.NewRecorder()
		problem.Render(rec, p)
		if strings.Contains(rec.Body.String(), c.notEchoed) {
			t.Errorf("%q: the response echoes the header: %s", c.ctype, rec.Body.String())
		}
	}
}

// Past the limit the server must close the connection rather than drain the
// rest of the body: MaxBodyBytes gives http.MaxBytesReader the response
// writer, which tells net/http to. The body stays under the 256 KiB net/http
// drains on its own, so a keep-alive here means the server read it all.
func TestMaxBodyBytesClosesTheConnection(t *testing.T) {
	h := nanohttp.MaxBodyBytes(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := nanohttp.Decode(r, &body{}); err != nil {
			problem.Write(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	big := `{"name":"` + strings.Repeat("x", 64<<10) + `"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !resp.Close {
		t.Fatal("the server must close the connection after an oversized body")
	}
}
