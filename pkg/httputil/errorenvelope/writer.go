package errorenvelope

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
	"github.com/nanostack-dev/nanostack-framework/pkg/fault"
)

// Log keys of the cause of an unmodelled error, as
// modules/httpserver.StrictErrorHandler writes them.
const (
	KeyErrorType   = "error_type"
	KeyErrorDetail = "error_detail"
)

// Writer writes the envelope for the error hooks of a strict server and for
// the router, and logs each error once through core/log.
//
//	envelope := errorenvelope.New(errorenvelope.Options{Logger: logger})
//	handler := api.NewHTTPHandler(server, api.HTTPHandlerOptions{
//		RequestErrorHandlerFunc:  envelope.RequestError,
//		ResponseErrorHandlerFunc: envelope.ResponseError,
//	})
//
// The same two methods fit the RequestErrorHandlerFunc and
// ResponseErrorHandlerFunc of oapi-codegen's chi strict server.
type Writer struct {
	opts Options
}

// New returns a Writer with opts.
func New(opts Options) *Writer {
	return &Writer{opts: opts}
}

// RequestError writes the request-hook error err (body decoding, generated
// validation, parameter binding) as ResolveRequest maps it.
func (w *Writer) RequestError(rw http.ResponseWriter, r *http.Request, err error) {
	w.write(rw, r, err, SourceRequest, ResolveRequest(r, err, w.opts))
}

// ResponseError writes the error that a handler returned as Resolve maps it.
//
// For a stream that already sent its status, the failure line reports the
// status the client received, not the one of the error.
func (w *Writer) ResponseError(rw http.ResponseWriter, r *http.Request, err error) {
	w.write(rw, r, err, SourceResponse, Resolve(r, err, w.opts))
}

// Handler returns a handler that writes apiErr and logs it. A router passes
// it as its NotFound or MethodNotAllowed handler to keep a service's own
// answer, for example echopoint's 400 for a path that matches no operation:
//
//	nanohttp.NewRouter(mux, nanohttp.RouterOptions{
//		NotFound: envelope.Handler(fault.BadRequest("BAD_REQUEST", "no matching operation was found")),
//	})
func (w *Writer) Handler(apiErr *fault.Error) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.write(rw, r, apiErr, SourceRouter, Response{Status: apiErr.HTTPStatus(), Body: apiErr})
	})
}

// NotFound is the router's NotFound handler: 404 NOT_FOUND.
func (w *Writer) NotFound() http.Handler {
	return w.Handler(fault.NewWithStatus(CodeNotFound, "No route matches the request path.", http.StatusNotFound))
}

// MethodNotAllowed is the router's MethodNotAllowed handler: 405
// METHOD_NOT_ALLOWED. The router has set the Allow header.
func (w *Writer) MethodNotAllowed() http.Handler {
	return w.Handler(fault.NewWithStatus(CodeMethodNotAllowed,
		"The route does not accept this method; the Allow header lists those it accepts.",
		http.StatusMethodNotAllowed))
}

func (w *Writer) logger() *slog.Logger {
	if w.opts.Logger != nil {
		return w.opts.Logger
	}
	return slog.Default()
}

// write writes resp, then logs the failure through Options.Log or log.
func (w *Writer) write(rw http.ResponseWriter, r *http.Request, err error, source Source, resp Response) {
	f := Failure{Err: err, Source: source, Response: resp, SentStatus: resp.Status}
	if resp.Stream {
		// Read before Write, which marks the response as started.
		if sent, written, known := log.ResponseStatus(rw); known && written {
			f.SentStatus = sent
		}
	}
	f.Written = resp.Write(rw)
	if w.opts.Log != nil {
		w.opts.Log(r, f)
		return
	}
	w.log(r, f)
	if !f.Written {
		w.logger().
			DebugContext(r.Context(), "the stream error frame is not sent: the response started with another content type",
				slog.String("content_type", rw.Header().Get("Content-Type")))
	}
}

// log writes one "request failed" line with the request context, under the
// keys of http/problem.Write: error, status and route. core/log picks the
// level from the error and the status the client received (log.LevelFor):
// Debug for a 4xx, Warn for an abandoned request, Error for a 5xx. An
// unmodelled error also carries its Go type and its verbose form, the only
// record of a 5xx cause.
func (w *Writer) log(r *http.Request, f Failure) {
	err, resp := f.Err, f.Response
	if resp.Stream && resp.Abandoned {
		w.logger().DebugContext(r.Context(), "stream closed by client", log.Err(err))
		return
	}
	ctx := r.Context()
	if resp.Abandoned && !log.IsContextError(err) {
		// A handler that gives up on a cancelled context often returns its
		// driver's error. Join the cancellation, so the line is Warn.
		err = errors.Join(err, ctx.Err())
	}
	var attrs []any
	if r.Pattern != "" {
		attrs = append(attrs, slog.String(log.KeyRoute, r.Pattern))
	}
	if resp.Unmodelled {
		attrs = append(attrs,
			slog.String(KeyErrorType, fmt.Sprintf("%T", err)),
			slog.String(KeyErrorDetail, fmt.Sprintf("%+v", err)))
	}
	log.ErrorStatusTo(ctx, w.logger(), err, f.SentStatus, "request failed", attrs...)
}

// StrictErrorHandlerLog returns an Options.Log that writes the lines of
// modules/httpserver.StrictErrorHandler, which the alerts and contract suites
// of the services that used it read, through logger (nil means slog.Default()
// at each error):
//
//   - a request-hook error (SourceRequest): no line; the request summary
//     records the 400;
//   - a request the client abandoned: Warn "Request abandoned by client";
//   - an unmodelled error: Error "Unhandled error returned by strict handler";
//   - another 5xx: Error "Internal server error";
//   - another status: Info "Request error", the router's answers included;
//   - on a stream: Debug "SSE stream closed by client" when the client left,
//     Error "SSE stream error" for a 5xx, Info "SSE stream warning" otherwise,
//     then Debug "SSE stream headers already written, cannot modify response"
//     when the frame was not written.
//
// Each line but the stream's Debug lines carries error and status, the status
// of the error rather than the one a started stream sent; Error lines also
// carry error_type and error_detail. The level is fixed by the case, not
// picked by log.LevelFor.
func StrictErrorHandlerLog(logger *slog.Logger) func(*http.Request, Failure) {
	return func(r *http.Request, f Failure) {
		l := logger
		if l == nil {
			l = slog.Default()
		}
		ctx, resp := r.Context(), f.Response
		status := slog.Int(log.KeyStatus, resp.Status)
		switch {
		case f.Source == SourceRequest:
		case resp.Stream && resp.Abandoned:
			l.DebugContext(ctx, "SSE stream closed by client")
		case resp.Stream && resp.Status >= http.StatusInternalServerError:
			l.ErrorContext(ctx, "SSE stream error", append(internalErrorAttrs(f.Err), status)...)
		case resp.Stream:
			l.InfoContext(ctx, "SSE stream warning", log.Err(f.Err), status)
		case resp.Abandoned:
			l.WarnContext(ctx, "Request abandoned by client", log.Err(f.Err), status)
		case resp.Unmodelled:
			l.ErrorContext(ctx, "Unhandled error returned by strict handler",
				append(internalErrorAttrs(f.Err), status)...)
		case resp.Status >= http.StatusInternalServerError:
			l.ErrorContext(ctx, "Internal server error", append(internalErrorAttrs(f.Err), status)...)
		default:
			l.InfoContext(ctx, "Request error", log.Err(f.Err), status)
		}
		if resp.Stream && !resp.Abandoned && !f.Written {
			l.DebugContext(ctx, "SSE stream headers already written, cannot modify response")
		}
	}
}

// internalErrorAttrs are the attributes of a server error line: the error,
// its Go type and its verbose form, which surfaces the wrapped chain and any
// stack trace.
func internalErrorAttrs(err error) []any {
	return []any{
		log.Err(err),
		slog.String(KeyErrorType, fmt.Sprintf("%T", err)),
		slog.String(KeyErrorDetail, fmt.Sprintf("%+v", err)),
	}
}

// PlainRequestError answers 400 text/plain, as http.Error does, with the
// first line of the parameter-binding error that err wraps, or of err when it
// wraps none: anchor's answer to parameter-binding and validator errors
// today. On the nano stack the binding error is the cause of the
// problem.InvalidParameter or problem.MissingParameter that the generated
// server passes. A boundary problem without a cause, such as a validation
// failure, answers its first field error as "<location>: <detail>".
func PlainRequestError(rw http.ResponseWriter, _ *http.Request, err error) {
	http.Error(rw, firstLine(plainText(err)), http.StatusBadRequest)
}

func plainText(err error) string {
	if err == nil {
		return http.StatusText(http.StatusBadRequest)
	}
	p, ok := errors.AsType[*problem.Error](err)
	if !ok {
		return err.Error()
	}
	if cause := p.Unwrap(); cause != nil {
		return cause.Error()
	}
	if fields := p.FieldErrors(); len(fields) > 0 {
		return fieldDetail("", fields[0]).Message
	}
	return p.ProblemDetail()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
