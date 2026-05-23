package jet

import "testing"

func TestDefaultServiceConfig(t *testing.T) {
	cfg := DefaultServiceConfig("echopoint", "ECHOPOINT")
	if cfg.Databases[0].OutputDir != "./internal/db/gen" {
		t.Fatalf("unexpected output dir %q", cfg.Databases[0].OutputDir)
	}
	if cfg.EnvNames.Password != "ECHOPOINT_DB_PASSWORD" {
		t.Fatalf("unexpected password env %q", cfg.EnvNames.Password)
	}
}
