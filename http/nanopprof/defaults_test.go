package nanopprof_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/http/nanopprof"
)

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanopprof.DefaultConfig())
}
