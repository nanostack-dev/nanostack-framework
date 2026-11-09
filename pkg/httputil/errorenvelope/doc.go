// Package errorenvelope writes the legacy error envelope of echopoint and
// anchor, {"errors":[{"code","message","metadata"}]} with Content-Type
// application/json, for every error the HTTP boundary produces (design C3 of
// docs/design/beta-readiness.md).
//
// It plugs into the hooks that already receive those errors: the strict
// server's RequestErrorHandlerFunc and ResponseErrorHandlerFunc (the same two
// hooks in oapi-codegen's chi strict server and in the nanohttp generated
// server), and the NotFound and MethodNotAllowed handlers of
// nanohttp.NewRouter. So a service moves its handlers to the nano HTTP stack
// without a change to what its clients, gate flows and alerts see.
//
// Resolve and ResolveRequest decide the status and the body. Writer writes
// them and logs one "request failed" line through core/log, with the request
// context, so the line carries the scope attributes of log.HTTPMiddleware
// (request_id, org_id, ...). modules/httpserver.StrictErrorHandler uses the
// same resolution and keeps its zerolog logging.
//
// The package lives in pkg because it renders pkg/fault.Error, the type the
// services' specs pin with x-go-type, and the db/search errors. It is a
// bridge: it goes away with pkg/fault, when the services answer
// application/problem+json (http/problem).
package errorenvelope
