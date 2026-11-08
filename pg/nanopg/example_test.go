package nanopg_test

import (
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/pg/nanopg"
)

// The generated Bind fills Config from the service's postgres section;
// NewDB opens the pool from its DSN. The DSN quotes every value.
func ExampleConfig_DSN() {
	cfg := nanopg.Config{
		Host: "db", Port: 5432, User: "billing", DB: "billing",
		Password: secret.New("it's secret"), SSLMode: "disable",
	}
	fmt.Println(cfg.DSN())
	// Output: host='db' port=5432 user='billing' dbname='billing' password='it\'s secret' sslmode='disable'
}
