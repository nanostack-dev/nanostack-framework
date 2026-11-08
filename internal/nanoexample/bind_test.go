package nanoexample

import (
	"testing"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/config"
)

// TestBindFromEnvironment binds the service Config, every nano module's
// section included, from the environment: the nested nano.AppConfig (APP_*),
// log.Settings with its process-global names (LOG_*), the module sections
// with names derived from their paths, and the module defaults.
func TestBindFromEnvironment(t *testing.T) {
	env := map[string]string{
		"POSTGRES_HOST": "db", "POSTGRES_USER": "app", "POSTGRES_DB": "billing", "POSTGRES_PASSWORD": "pw",
		"REDIS_ADDRESS": "redis:6379", "ENABLE_PPROF": "1", "APP_START_TIMEOUT": "42s",
		"LOG_LEVEL": "debug", "LOG_WIRE": "legacy", "SERVICE_NAME": "billing",
	}
	src, err := config.NewSources(config.EnvMap(env))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Bind(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Postgres.DB != "billing" || cfg.Postgres.Password.Reveal() != "pw" || cfg.Postgres.MaxOpenConns != 25 ||
		cfg.Redis.Address != "redis:6379" || !cfg.Pprof.Enabled || cfg.PGLock.LeaseDuration != 10*time.Second ||
		cfg.Tracing.SampleRatio != 1 || !cfg.Migrations.Enabled || cfg.HTTP.Port != 8080 {
		t.Fatalf("module sections = %+v", cfg)
	}
	if cfg.App.StartTimeout != 42*time.Second || cfg.App.StopTimeout != 15*time.Second ||
		cfg.App.HealthTimeout != 5*time.Second {
		t.Fatalf("App = %+v, want start 42s and the default stop and health timeouts", cfg.App)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Wire != "legacy" || cfg.Log.Service != "billing" ||
		cfg.Log.Environment != "development" {
		t.Fatalf("Log = %+v", cfg.Log)
	}
}
