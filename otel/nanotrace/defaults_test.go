package nanotrace_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/otel/nanotrace"
)

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanotrace.DefaultConfig())
}
