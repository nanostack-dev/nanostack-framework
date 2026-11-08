package problem

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// ContentType is the RFC 9457 media type of a problem response.
const ContentType = "application/problem+json"

// Write renders err as an application/problem+json response. It sets
// instance to the request path and trace_id to the request's trace, and logs
// the error with log.ErrorStatusTo on slog's default logger, with the request
// context, under log.KeyError, log.KeyStatus and log.KeyProblem. core/log
// picks the level from the error and the status, as log.LevelFor documents.
//
// When the request context is done and err carries no status of its own (no
// fault.Described), Write treats the failure as the cancellation or timeout:
// a handler that gives up on a cancelled context often returns its driver's
// error, such as "bad connection", that does not wrap context.Canceled. Write
// then answers 499 or 504 and logs err joined with the context's error, so
// the line is Warn like the request summary of log.HTTPMiddleware.
//
// The line also carries log.KeyRoute, the ServeMux pattern, when the router
// set one. The route, not the source, names the failed operation: a generated
// server calls Write after the handler has returned, so the handler is not on
// the stack, and the line's source is Write itself.
//
// Write never sends err.Error() unless err chose that text through
// ProblemDetail.
func Write(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	p := FromError(err)
	if ctxErr := ctx.Err(); ctxErr != nil && p.Status == http.StatusInternalServerError {
		if _, described := errors.AsType[fault.Described](err); !described {
			err = joinContext(err, ctxErr, context.Cause(ctx))
			p = FromError(err)
		}
	}
	p.Instance = r.URL.Path
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		p.TraceID = sc.TraceID().String()
	}

	attrs := []any{slog.String(log.KeyProblem, p.Type)}
	if r.Pattern != "" {
		attrs = append(attrs, slog.String(log.KeyRoute, r.Pattern))
	}
	log.ErrorStatusTo(ctx, nil, err, p.Status, "request failed", attrs...)

	Render(w, p)
}

// joinContext joins err with the context's error, and with its cause when the
// cause is something else.
func joinContext(err, ctxErr, cause error) error {
	if errors.Is(err, ctxErr) {
		return err
	}
	if cause == nil || errors.Is(cause, ctxErr) {
		return errors.Join(err, ctxErr)
	}
	return errors.Join(err, ctxErr, cause)
}

// Render writes p as the response, with its status and media type.
func Render(w http.ResponseWriter, p Problem) {
	body, err := jsonv2.Marshal(p)
	if err != nil {
		internal := Internal()
		internal.Instance, internal.TraceID = p.Instance, p.TraceID
		body, _ = jsonv2.Marshal(internal)
		p.Status = internal.Status
	}
	h := w.Header()
	h.Set("Content-Type", ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}
