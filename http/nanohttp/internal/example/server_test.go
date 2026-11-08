package example_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/nanostack-dev/nanostack-framework/http/nanohttp"
	"github.com/nanostack-dev/nanostack-framework/http/nanohttp/internal/example"
)

// notFound stands in for a generated error catalogue entry.
type notFound struct{ id string }

func (e notFound) Error() string { return "webhook " + e.id + " not found in shard 7" }

func (notFound) ProblemType() string {
	return "https://errors.nanostack.dev/webhooks/webhook_not_found"
}

func (notFound) ProblemStatus() int { return http.StatusNotFound }

func (notFound) ProblemTitle() string { return "Webhook not found" }

func (e notFound) ProblemDetail() string { return "No webhook has id " + e.id + "." }

type server struct {
	gotBody chan *example.UpdateWebhookRequest
}

func (s server) CreateWebhook(
	_ context.Context,
	req example.CreateWebhookRequestObject,
) (example.CreateWebhookResponseObject, error) {
	if req.Body.Name == "boom" {
		return nil, errors.New("pq: connection to 10.0.0.3 refused")
	}
	return example.CreateWebhook201JSONResponse{Id: "wh_1", Name: req.Body.Name, Url: req.Body.Url}, nil
}

func (s server) GetWebhook(
	_ context.Context,
	req example.GetWebhookRequestObject,
) (example.GetWebhookResponseObject, error) {
	if req.Id != "wh_1" {
		return nil, fmt.Errorf("get: %w", notFound{req.Id})
	}
	return example.GetWebhook200JSONResponse{Id: "wh_1", Name: "a", Url: "https://x.dev"}, nil
}

func (s server) UpdateWebhook(
	_ context.Context,
	req example.UpdateWebhookRequestObject,
) (example.UpdateWebhookResponseObject, error) {
	s.gotBody <- req.Body
	return example.UpdateWebhook200JSONResponse{Id: req.Id, Name: "a", Url: "https://x.dev"}, nil
}

func (s server) ListEvents(context.Context, example.ListEventsRequestObject) (example.ListEventsResponseObject, error) {
	return example.ListEvents200JSONResponse{}, nil
}

func (s server) AddNote(context.Context, example.AddNoteRequestObject) (example.AddNoteResponseObject, error) {
	return example.AddNote204Response{}, nil
}

func newServer() (http.Handler, server) {
	s := server{gotBody: make(chan *example.UpdateWebhookRequest, 1)}
	return nanohttp.MaxBodyBytes(4096)(example.NewHTTPHandler(s, example.HTTPHandlerOptions{})), s
}
