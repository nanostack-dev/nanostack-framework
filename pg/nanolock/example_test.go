package nanolock_test

import (
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
)

// The defaults of Config are in its tags; the generated Bind applies them. A
// Config built by hand, in a test or an Override, starts from DefaultConfig
// so the boolean defaults apply too. NewClient also defaults a zero
// duration.
func ExampleConfig() {
	cfg := nanolock.DefaultConfig()
	cfg.TableName = "billing_locks"
	fmt.Println(cfg.TableName, cfg.LeaseDuration, cfg.AutoCreateTable)
	// Output: billing_locks 10s true
}
