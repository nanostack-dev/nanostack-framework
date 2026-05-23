package jet

type DBConfig struct {
	Name      string
	User      string
	Password  string
	Port      int
	DBName    string
	Schema    string
	SSLMode   string
	OutputDir string
}

type AdminDBConfig struct {
	User     string
	Password string
	Port     int
	SSLMode  string
}

type EnvNames struct {
	Password string
	Port     string
	DBName   string
}

type Config struct {
	Admin       AdminDBConfig
	Databases   []DBConfig
	EnvNames    EnvNames
	TextArrayAs string
}

// DefaultServiceConfig returns the Nanostack go-jet generation convention.
func DefaultServiceConfig(serviceName string, envPrefix string) Config {
	return Config{
		Admin: AdminDBConfig{User: "postgres", Port: 5432, SSLMode: "disable"},
		Databases: []DBConfig{{
			Name:      serviceName,
			User:      "postgres",
			Port:      5432,
			DBName:    serviceName,
			Schema:    "public",
			SSLMode:   "disable",
			OutputDir: "./internal/db/gen",
		}},
		EnvNames: EnvNames{
			Password: envPrefix + "_DB_PASSWORD",
			Port:     envPrefix + "_DB_PORT",
			DBName:   envPrefix + "_DB_NAME",
		},
		TextArrayAs: "github.com/lib/pq.StringArray",
	}
}
