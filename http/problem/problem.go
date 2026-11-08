// Package problem renders errors as RFC 9457 application/problem+json.
//
// Every error path of an HTTP service ends in [Write]: request decoding and
// generated validation, parameter binding, and the errors handlers return.
// [FromError] maps an error to a [Problem] by its type, never by its message,
// and an error it does not recognise becomes a 500 that reveals nothing.
//
// An error controls its own response by implementing [fault.Described];
// generated error catalogues do. The optional [fault.Detailed] and
// [fault.Coded] interfaces add a client-facing detail and a stable code.
// Package fault owns those interfaces and the type URI base; this package
// only renders them. Error() is never sent to a client, because it usually
// carries wrapped internal causes.
package problem

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
)

// typeBase prefixes the type URI of every framework problem, with the
// separator rule of package fault: <base>/<namespace>/<name>.
const typeBase = fault.DefaultTypeBase + "/http/"

// Problem type URIs owned by the framework.
const (
	TypeValidation           = typeBase + "validation_failed"
	TypeInvalidBody          = typeBase + "invalid_body"
	TypeMalformedJSON        = typeBase + "malformed_json"
	TypeMissingBody          = typeBase + "missing_body"
	TypeBodyTooLarge         = typeBase + "body_too_large"
	TypeUnsupportedMediaType = typeBase + "unsupported_media_type"
	TypeInvalidParameter     = typeBase + "invalid_parameter"
	TypeBadRequest           = typeBase + "bad_request"
	TypeTimeout              = typeBase + "timeout"
	TypeClientClosed         = typeBase + "client_closed_request"
	TypeInternal             = typeBase + "internal"
)

// StatusClientClosedRequest is the nginx convention for a request the client
// abandoned before the response was written.
const StatusClientClosedRequest = 499

// Problem is an RFC 9457 problem details object plus the framework's
// extension members code, trace_id and errors.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	// Code is a stable machine-readable identifier, such as
	// "http.validation_failed".
	Code string `json:"code,omitempty"`
	// TraceID joins the response to the server's trace and logs.
	TraceID string `json:"trace_id,omitempty"`
	// Errors lists every violation for validation and decoding problems.
	Errors []FieldError `json:"errors,omitempty"`
}

// FieldError is one violation inside a request. Exactly one of Pointer and
// Parameter locates it: Pointer for the body, Parameter for a path, query,
// header or cookie parameter.
type FieldError struct {
	// Pointer is the RFC 6901 JSON Pointer into the request body; "" is the
	// whole body.
	Pointer string
	// Parameter names the request parameter.
	Parameter string
	// Code is the JSON Schema keyword that failed, or a decoding code: one of
	// the Code constants of core/validate.
	Code string
	// Detail describes the violation for a human.
	Detail string
}

// MarshalJSONTo always writes pointer for a body error, including the root
// pointer "", and parameter for a parameter error.
func (f FieldError) MarshalJSONTo(enc *jsontext.Encoder) error {
	type body struct {
		Pointer string `json:"pointer"`
		Code    string `json:"code"`
		Detail  string `json:"detail"`
	}
	type param struct {
		Parameter string `json:"parameter"`
		Code      string `json:"code"`
		Detail    string `json:"detail"`
	}
	if f.Parameter != "" {
		return jsonv2.MarshalEncode(enc, param{f.Parameter, f.Code, f.Detail})
	}
	return jsonv2.MarshalEncode(enc, body{f.Pointer, f.Code, f.Detail})
}

// UnmarshalJSONFrom reads a FieldError written by MarshalJSONTo.
func (f *FieldError) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var v struct {
		Pointer   string `json:"pointer"`
		Parameter string `json:"parameter"`
		Code      string `json:"code"`
		Detail    string `json:"detail"`
	}
	if err := jsonv2.UnmarshalDecode(dec, &v); err != nil {
		return err
	}
	*f = FieldError{Pointer: v.Pointer, Parameter: v.Parameter, Code: v.Code, Detail: v.Detail}
	return nil
}

// FromError maps err to a Problem, most specific first:
//
//   - a fault.Described error in the chain (the outermost one wins), with its
//     detail, code and field errors when it provides them;
//   - *http.MaxBytesError → 413;
//   - context.DeadlineExceeded → 504, context.Canceled → 499;
//   - anything else, including nil → 500 with no detail.
//
// Instance and TraceID are left for the caller, which knows the request.
func FromError(err error) Problem {
	if d, ok := errors.AsType[fault.Described](err); ok {
		if p, valid := fromDescribed(d); valid {
			return p
		}
	}
	if mb, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return FromError(BodyTooLarge(mb.Limit))
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Problem{
			Type:   TypeTimeout,
			Title:  "Gateway Timeout",
			Status: http.StatusGatewayTimeout,
			Code:   "http.timeout",
		}
	case errors.Is(err, context.Canceled):
		return Problem{
			Type:   TypeClientClosed,
			Title:  "Client Closed Request",
			Status: StatusClientClosedRequest,
			Code:   "http.client_closed_request",
		}
	}
	return Internal()
}

// Internal is the 500 problem for an unrecognised error. It reveals nothing
// about the cause.
func Internal() Problem {
	return Problem{
		Type:   TypeInternal,
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Code:   "http.internal",
	}
}

func fromDescribed(d fault.Described) (Problem, bool) {
	if !fault.Valid(d) {
		return Problem{}, false
	}
	status := d.ProblemStatus()
	p := Problem{Type: d.ProblemType(), Title: d.ProblemTitle(), Status: status}
	if p.Title == "" {
		p.Title = http.StatusText(status)
	}
	if v, ok := d.(fault.Detailed); ok {
		p.Detail = v.ProblemDetail()
	}
	if v, ok := d.(fault.Coded); ok {
		p.Code = v.ProblemCode()
	}
	if v, ok := d.(fieldErrorer); ok {
		p.Errors = v.problemErrors()
	}
	return p, true
}

// fieldErrorer is implemented by the framework's own problems that list
// violations.
type fieldErrorer interface {
	problemErrors() []FieldError
}
