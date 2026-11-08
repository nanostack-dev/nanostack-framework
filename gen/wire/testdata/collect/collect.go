// Package collect is a service whose manifest uses nano.Collect the way
// echopoint uses fx value groups, and whose test reads constructed values
// with BuildComponents the way echopoint's suite uses fx.Populate.
package collect

import (
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/logging"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/collect/workflow"
)

func NewBillingDefinition() *workflow.Definition { return &workflow.Definition{Name: "billing"} }

// UseLogger stands in for code that needs the enriched logger at boot.
func UseLogger(*logging.Enriched) {}
