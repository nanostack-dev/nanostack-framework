package nanoserver_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanoserver.DefaultConfig())
}
