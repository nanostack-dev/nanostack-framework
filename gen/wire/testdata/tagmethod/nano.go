//go:build nanogen

package tagmethod

import (
	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/gen/wire/testdata/tagmethod/api"
)

var App = nano.Manifest("tagmethod",
	nano.Providers(
		api.NewAuthorsHandler,
		api.NewBooksHandler,
		api.NewHandlers,
		NewServer,
	),
)
