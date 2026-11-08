// Package golden_test serves every golden package through its generated
// std-http strict server and checks two things for each request:
//
//   - the response: status and every violation (pointer or parameter, code);
//   - agreement with kin-openapi, an independent validator that walks the
//     spec: the generated validators accept exactly what kin accepts, except
//     for the documented divergences where kin is not the reference.
//
// kin-openapi is a test-only dependency; nothing outside _test.go files
// imports it.
package golden_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/additional"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/arrays"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/enums"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/formats"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nested"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nullable"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nullable31"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/nullabletype"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/numbers"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/numbers31"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/params"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/required"
	goldenstrings "github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/strings"
	"github.com/nanostack-dev/nanostack-framework/gen/validate/internal/golden/unions"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// Each golden package's strict server answers 204 once the request passes.
type (
	requiredImpl   struct{}
	numbersImpl    struct{}
	numbers31Impl  struct{}
	stringsImpl    struct{}
	formatsImpl    struct{}
	enumsImpl      struct{}
	arraysImpl     struct{}
	nestedImpl     struct{}
	nullableImpl   struct{}
	nullable31Impl struct{}
	nullableTImpl  struct{}
	unionsImpl     struct{}
	additionalImpl struct{}
	paramsImpl     struct{}
)

func (requiredImpl) Check(context.Context, required.CheckRequestObject) (required.CheckResponseObject, error) {
	return required.Check204Response{}, nil
}
func (numbersImpl) Check(context.Context, numbers.CheckRequestObject) (numbers.CheckResponseObject, error) {
	return numbers.Check204Response{}, nil
}
func (numbers31Impl) Check(context.Context, numbers31.CheckRequestObject) (numbers31.CheckResponseObject, error) {
	return numbers31.Check204Response{}, nil
}
func (stringsImpl) Check(context.Context, goldenstrings.CheckRequestObject) (goldenstrings.CheckResponseObject, error) {
	return goldenstrings.Check204Response{}, nil
}
func (formatsImpl) Check(context.Context, formats.CheckRequestObject) (formats.CheckResponseObject, error) {
	return formats.Check204Response{}, nil
}
func (enumsImpl) Check(context.Context, enums.CheckRequestObject) (enums.CheckResponseObject, error) {
	return enums.Check204Response{}, nil
}
func (arraysImpl) Check(context.Context, arrays.CheckRequestObject) (arrays.CheckResponseObject, error) {
	return arrays.Check204Response{}, nil
}
func (nestedImpl) Check(context.Context, nested.CheckRequestObject) (nested.CheckResponseObject, error) {
	return nested.Check204Response{}, nil
}
func (nullableImpl) Check(context.Context, nullable.CheckRequestObject) (nullable.CheckResponseObject, error) {
	return nullable.Check204Response{}, nil
}
func (nullable31Impl) Check(context.Context, nullable31.CheckRequestObject) (nullable31.CheckResponseObject, error) {
	return nullable31.Check204Response{}, nil
}
func (nullableTImpl) Check(
	context.Context,
	nullabletype.CheckRequestObject,
) (nullabletype.CheckResponseObject, error) {
	return nullabletype.Check204Response{}, nil
}

func (unionsImpl) Check(context.Context, unions.CheckRequestObject) (unions.CheckResponseObject, error) {
	return unions.Check204Response{}, nil
}
func (additionalImpl) Check(context.Context, additional.CheckRequestObject) (additional.CheckResponseObject, error) {
	return additional.Check204Response{}, nil
}
func (paramsImpl) ListItems(context.Context, params.ListItemsRequestObject) (params.ListItemsResponseObject, error) {
	return params.ListItems204Response{}, nil
}

var servers = map[string]http.Handler{
	"required":     required.NewHTTPHandler(requiredImpl{}, required.HTTPHandlerOptions{}),
	"numbers":      numbers.NewHTTPHandler(numbersImpl{}, numbers.HTTPHandlerOptions{}),
	"numbers31":    numbers31.NewHTTPHandler(numbers31Impl{}, numbers31.HTTPHandlerOptions{}),
	"strings":      goldenstrings.NewHTTPHandler(stringsImpl{}, goldenstrings.HTTPHandlerOptions{}),
	"formats":      formats.NewHTTPHandler(formatsImpl{}, formats.HTTPHandlerOptions{}),
	"enums":        enums.NewHTTPHandler(enumsImpl{}, enums.HTTPHandlerOptions{}),
	"arrays":       arrays.NewHTTPHandler(arraysImpl{}, arrays.HTTPHandlerOptions{}),
	"nested":       nested.NewHTTPHandler(nestedImpl{}, nested.HTTPHandlerOptions{}),
	"nullable":     nullable.NewHTTPHandler(nullableImpl{}, nullable.HTTPHandlerOptions{}),
	"nullable31":   nullable31.NewHTTPHandler(nullable31Impl{}, nullable31.HTTPHandlerOptions{}),
	"nullabletype": nullabletype.NewHTTPHandler(nullableTImpl{}, nullabletype.HTTPHandlerOptions{}),
	"unions":       unions.NewHTTPHandler(unionsImpl{}, unions.HTTPHandlerOptions{}),
	"additional":   additional.NewHTTPHandler(additionalImpl{}, additional.HTTPHandlerOptions{}),
	"params":       params.NewHTTPHandler(paramsImpl{}, params.HTTPHandlerOptions{}),
}

func TestGeneratedValidatorsMatchSpec(t *testing.T) {
	for _, c := range cases {
		t.Run(c.pkg+"/"+c.name, func(t *testing.T) {
			method, target := c.method, c.target
			if method == "" {
				method, target = http.MethodPost, "/check"
			}
			rec := httptest.NewRecorder()
			servers[c.pkg].ServeHTTP(rec, newRequest(method, target, c.header, c.body))

			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d\n%s", rec.Code, c.status, rec.Body.String())
			}
			if got := violations(t, rec); !equal(got, c.errs) {
				t.Errorf("violations = %q\nwant         %q", got, c.errs)
			}

			kinErr := kinValidate(t, c.pkg, newRequest(method, target, c.header, c.body))
			ours, kins := rec.Code < 300, kinErr == nil
			switch {
			case c.kinDiffers != "" && ours == kins:
				t.Errorf("kin-openapi now agrees; drop the divergence %q", c.kinDiffers)
			case c.kinDiffers == "" && ours != kins:
				t.Errorf("generated validators accept=%v, kin-openapi accept=%v: %v", ours, kins, kinErr)
			}
		})
	}
}

func newRequest(method, target string, header map[string]string, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		r.Header.Set(k, v)
	}
	return r
}

func violations(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code < 300 {
		return nil
	}
	if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Fatalf("content type = %q, body %s", ct, rec.Body.String())
	}
	var p problem.Problem
	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	var out []string
	for _, e := range p.Errors {
		loc := e.Pointer
		if e.Parameter != "" {
			loc = "?" + e.Parameter
		} else if loc == "" {
			loc = "/"
		}
		out = append(out, loc+" "+e.Code)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type kinSpec struct {
	doc    *openapi3.T
	router routers.Router
}

var (
	kinOnce  sync.Once
	kinSpecs = map[string]*kinSpec{}
	kinMu    sync.Mutex
)

func kinValidate(t *testing.T, pkg string, r *http.Request) error {
	t.Helper()
	kinOnce.Do(defineKinFormats)
	kinMu.Lock()
	ks, ok := kinSpecs[pkg]
	if !ok {
		doc, err := openapi3.NewLoader().LoadFromFile(pkg + "/openapi.yaml")
		if err != nil {
			kinMu.Unlock()
			t.Fatalf("kin load: %v", err)
		}
		doc.Servers = nil
		router, err := gorillamux.NewRouter(doc)
		if err != nil {
			kinMu.Unlock()
			t.Fatalf("kin router: %v", err)
		}
		ks = &kinSpec{doc: doc, router: router}
		kinSpecs[pkg] = ks
	}
	kinMu.Unlock()
	route, pathParams, err := ks.router.FindRoute(r)
	if err != nil {
		t.Fatalf("kin route: %v", err)
	}
	return openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{
		Request: r, PathParams: pathParams, Route: route,
		Options: &openapi3filter.Options{MultiError: true, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	})
}

// defineKinFormats gives kin format checks implemented independently of
// core/validate, so the cross-check is not circular.
func defineKinFormats() {
	define := func(name string, check func(string) bool) {
		openapi3.DefineStringFormatValidator(name, openapi3.NewCallbackValidator(func(s string) error {
			if !check(s) {
				return fmt.Errorf("not a valid %s", name)
			}
			return nil
		}))
	}
	uuid := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	define("uuid", uuid.MatchString)
	define("email", func(s string) bool {
		a, err := mail.ParseAddress(s)
		return err == nil && a.Address == s && a.Name == "" && regexp.MustCompile(`@[^.]+\.`).MatchString(s)
	})
	define("uri", func(s string) bool {
		u, err := url.Parse(s)
		return err == nil && u.Scheme != ""
	})
	hostname := regexp.MustCompile(
		`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`,
	)
	define("hostname", hostname.MatchString)
	openapi3.DefineIPv4Format()
	openapi3.DefineIPv6Format()
}
