// Package handler serves SearchWindows: nanostack gen repo writes the API
// mapper of search Window here, in window_search_api.gen.go.
package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/apibind"
	"github.com/nanostack-dev/nanostack-framework/gen/repo/testdata/apibind/apigen"
)

// Server is the strict server of the API.
type Server struct{}

// SearchWindows maps the body and sets the folder that the service
// resolves.
func (Server) SearchWindows(_ context.Context, req apigen.SearchWindowsRequestObject) (apibind.WindowSearch, error) {
	q := WindowSearchFromAPI(uuid.Nil, req.Body)
	if f := req.Body.Filter.FolderID; f != nil {
		q = q.ExternalID().Eq(*f)
	}
	return q, nil
}
