// Package auth contributes a second enricher of the same type, built from a
// value the module also provides.
package auth

import (
	"context"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/logging"
)

var Module = nano.Module("auth",
	nano.Providers(NewRepository, NewService),
	nano.Collect[logging.Enricher](NewPrincipalEnricher),
)

// Repository stands in for one of the repositories echopoint's integration
// suite pulls out with fx.Populate.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

type Service struct{ Repo *Repository }

func NewService(repo *Repository) *Service { return &Service{Repo: repo} }

func (s *Service) principal(context.Context) []slog.Attr {
	return []slog.Attr{slog.String("principal", "ada")}
}

// NewPrincipalEnricher adds the provided *Service to the collection.
func NewPrincipalEnricher(s *Service) logging.Enricher { return s.principal }
