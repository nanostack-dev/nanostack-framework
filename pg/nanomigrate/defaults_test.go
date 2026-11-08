package nanomigrate_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/config/configtest"
	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

func TestDefaultConfigMatchesTheDefaultTags(t *testing.T) {
	configtest.AssertDefaults(t, nanomigrate.DefaultConfig())
}
