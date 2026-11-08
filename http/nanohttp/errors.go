package nanohttp

import (
	"errors"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/core/fault"
	"github.com/nanostack-dev/nanostack-framework/http/problem"
)

// RequestError is the strict server's RequestErrorHandlerFunc. Decode
// errors are already problems; any other request error (a form or multipart
// body that does not parse) becomes a 400 that does not echo the cause.
func RequestError(w http.ResponseWriter, r *http.Request, err error) {
	if _, ok := errors.AsType[fault.Described](err); !ok {
		err = problem.BadRequest(err)
	}
	problem.Write(w, r, err)
}

// ResponseError is the strict server's ResponseErrorHandlerFunc: the error a
// handler returned, or a failure to write its response.
func ResponseError(w http.ResponseWriter, r *http.Request, err error) {
	problem.Write(w, r, err)
}
