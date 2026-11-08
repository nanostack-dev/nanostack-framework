package signatures

import "github.com/nanostack-dev/nanostack-framework/core/nano"

type Config struct {
	App  nano.AppConfig
	DB   DBConfig
	Name string
}

type DBConfig struct{ DSN string }
