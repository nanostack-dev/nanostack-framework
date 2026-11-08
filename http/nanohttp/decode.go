// Package nanohttp is the HTTP boundary between net/http and the handlers
// oapi-codegen generates. It decodes request bodies strictly, runs the
// validators nano gen validate generates, and sends every error through
// problem.Write.
//
// The generated strict server calls Decode through the template override in
// package oapitemplates; services rarely call it directly.
package nanohttp

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/validate"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// Validator is implemented by every request body type: nano gen validate
// generates the method from the OpenAPI schema.
type Validator interface {
	Validate(validate.Path) validate.Errors
}

// DefaultMaxBodyBytes bounds a request body when no MaxBodyBytes middleware
// sets another limit.
const DefaultMaxBodyBytes int64 = 1 << 20

// ErrNoBody marks the error Decode returns for an empty body. A generated
// handler with an optional body checks it with errors.Is and leaves the body
// nil; for a required body the same error renders as a 400 problem.
var ErrNoBody = errors.New("nanohttp: empty request body")

// Decode reads the JSON request body into dst and validates it.
//
// The body is read up to the request's limit (413 beyond it) and must be
// application/json or a +json type when a Content-Type is sent: a
// Content-Type that does not parse is a 400 InvalidParameter problem, a
// well-formed other type a 415. It is decoded with json/v2 rejecting duplicate member names
// and invalid UTF-8 and matching names case-sensitively. A syntax error is a
// 400 MalformedJSON problem; a value of the wrong type is a 400 InvalidBody
// problem with its JSON Pointer. dst.Validate then runs with the document
// attached, so required, null and additionalProperties are exact, and every
// violation comes back at once as a 422 Validation problem.
//
// The body is buffered rather than streamed so the validators can consult
// the document; the size limit bounds the buffer.
func Decode[T Validator](r *http.Request, dst T) error {
	data, err := ReadBody(r)
	if err != nil {
		return err
	}
	if decodeErr := jsonv2.Unmarshal(data, dst, validate.DecodeOptions()); decodeErr != nil {
		if se, ok := errors.AsType[*jsonv2.SemanticError](decodeErr); ok {
			return problem.InvalidBody(string(se.JSONPointer), decodeErr)
		}
		return problem.MalformedJSON(decodeErr)
	}
	if errs := dst.Validate(validate.RootOf(data)); len(errs) > 0 {
		return problem.Validation(errs)
	}
	return nil
}

// Unmarshal decodes the JSON text data into v with the strict options of
// Decode and of the generated validators (validate.DecodeOptions). The union
// template override calls it in the As<Variant> methods, so a handler reads
// a union exactly as the validators checked it: a member whose name differs
// only in case, such as "TOKEN" next to "token", is not read into the field.
func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, validate.DecodeOptions())
}

// ReadBody checks the media type and reads the whole body within the
// request's size limit. An empty or blank body is ErrNoBody.
func ReadBody(r *http.Request) ([]byte, error) {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		switch {
		case err != nil:
			return nil, problem.InvalidParameter("Content-Type", fmt.Errorf("nanohttp: parse content type: %w", err))
		case !isJSON(mt):
			return nil, problem.UnsupportedMediaType(mt)
		}
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil, noBody()
	}
	// MaxBodyBytes has already wrapped the body with the response writer,
	// so the server closes the connection at the limit. This wrap is the
	// backstop for a request it did not wrap.
	limit := maxBodyBytes(r.Context())
	data, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, limit))
	if err != nil {
		if mb, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, fmt.Errorf("nanohttp: read body: %w", errors.Join(mb, problem.BodyTooLarge(mb.Limit)))
		}
		return nil, problem.MalformedJSON(fmt.Errorf("nanohttp: read body: %w", err))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, noBody()
	}
	return data, nil
}

func noBody() error {
	return fmt.Errorf("%w: %w", ErrNoBody, problem.MissingBody())
}

// isJSON reports whether the parsed media type mt is application/json or
// ends in +json, as RFC 6839 defines.
func isJSON(mt string) bool {
	return mt == "application/json" || strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json")
}

type maxBodyKey struct{}

// MaxBodyBytes returns middleware that limits the request bodies of the
// requests it wraps to limit bytes. It wraps the body with
// http.MaxBytesReader and the response writer, so that past the limit the
// server closes the connection instead of draining the rest of the body,
// and records the limit for the 413 problem Decode returns.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(context.WithValue(r.Context(), maxBodyKey{}, limit))
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func maxBodyBytes(ctx context.Context) int64 {
	if n, ok := ctx.Value(maxBodyKey{}).(int64); ok && n > 0 {
		return n
	}
	return DefaultMaxBodyBytes
}
