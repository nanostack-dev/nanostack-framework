package migrations_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/nanostack-dev/nanostack-framework/modules/migrations"
)

// TestValidateFalseLogsNoWarning checks that MigrationConfig.Validate alone
// turns the down-file check off, also while migrations are enabled.
func TestValidateFalseLogsNoWarning(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "billing", "migrations")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1_a.up.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, validate := range []bool{false, true} {
		var buf bytes.Buffer
		m := migrations.NewMigrationProvider(zerolog.New(&buf), nil, migrations.MigrationConfig{
			Enabled: true, Validate: validate, BasePath: base,
		})
		if err := m.ValidateMigrations(); err != nil {
			t.Fatal(err)
		}
		if logged := buf.Len() != 0; logged != validate {
			t.Fatalf("Validate=%v: log = %q, want a warning only when Validate is true", validate, buf.String())
		}
	}
}
