package nanolock_test

import (
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/pg/nanolock"
)

// The defaults of Config are in its tags; the generated Bind applies them.
// A Config built by hand sets every field.
func ExampleConfig() {
	cfg := nanolock.Config{TableName: "billing_locks", AutoCreateTable: true, TestOnStartup: true}
	fmt.Println(cfg.TableName)
	// Output: billing_locks
}
