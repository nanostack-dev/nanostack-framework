//go:build nanogen

package tagsplit

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/tagsplit/api"
)

var App = nano.Manifest("tagsplit",
	nano.Providers(
		api.NewBooksHandler,
		api.NewHandlers,
		NewServer,
	),
)
