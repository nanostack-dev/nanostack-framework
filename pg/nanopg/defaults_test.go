package nanopg_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
)

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanopg.DefaultConfig())
}
