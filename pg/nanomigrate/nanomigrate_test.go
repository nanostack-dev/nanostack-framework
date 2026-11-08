package nanomigrate_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pg/nanomigrate"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestMigrateDoesNothingWhenDisabled(t *testing.T) {
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: false, BasePath: "/does/not/exist"}, nil, discard())
	if err := m.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate with migrations disabled = %v, want nil", err)
	}
}

func TestMigrateFailsWithoutTheBasePath(t *testing.T) {
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: "/does/not/exist"}, nil, discard())
	err := m.Migrate(t.Context())
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Migrate = %v, want a missing base path error", err)
	}
}

func TestMigrateDoesNotStartAfterTheContextEnded(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, BasePath: t.TempDir()}, nil, discard())
	if err := m.Migrate(ctx); !errors.Is(err, nanomigrate.ErrStopped) {
		t.Fatalf("Migrate after cancel = %v, want ErrStopped", err)
	}
}

func TestValidateWarnsAboutAMissingDownFile(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "billing", "migrations")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"1_a.up.sql", "1_a.down.sql", "2_b.up.sql"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, Validate: true, BasePath: base}, nil, logger)

	if err := m.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.Count(out, "missing corresponding down migration file") != 1 ||
		!strings.Contains(out, `"missing_down":"2_b.down.sql"`) || !strings.Contains(out, `"module":"billing"`) {
		t.Fatalf("log = %s, want one warning for 2_b.down.sql in module billing", out)
	}
}

// TestValidateFalseLogsNoWarning checks that Config.Validate alone turns the
// down-file check off, also while migrations are enabled.
func TestValidateFalseLogsNoWarning(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "billing", "migrations")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1_a.up.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	m := nanomigrate.NewMigrator(nanomigrate.Config{Enabled: true, Validate: false, BasePath: base}, nil, logger)

	if err := m.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("log = %s, want nothing with Validate false", buf.String())
	}
}

func TestValidateOnlyWarnsAboutAMissingBasePath(t *testing.T) {
	m := nanomigrate.NewMigrator(
		nanomigrate.Config{Enabled: true, Validate: true, BasePath: "/does/not/exist"},
		nil,
		discard(),
	)
	if err := m.Validate(t.Context()); err != nil {
		t.Fatalf("Validate = %v, want nil: Migrate reports the missing path", err)
	}
}
