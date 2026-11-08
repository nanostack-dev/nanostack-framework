// Package syncs is a feature that contributes a workflow definition, like
// echopoint's ProvideSyncWorkflowDefinition.
package syncs

import (
	"sync/atomic"

	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/auth"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/workflow"
)

// Built counts ProvideSyncDefinition calls, so a test can see an override
// of the collection skip its members.
var Built atomic.Int64

type SyncService struct{ Repo *auth.Repository }

func NewSyncService(repo *auth.Repository) *SyncService { return &SyncService{Repo: repo} }

func ProvideSyncDefinition(*SyncService) *workflow.Definition {
	Built.Add(1)
	return &workflow.Definition{Name: "sync"}
}
