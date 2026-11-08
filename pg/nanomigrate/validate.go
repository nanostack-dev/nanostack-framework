package nanomigrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/log"
)

// Validate checks the layout <base>/<module>/migrations of a modular service
// and logs a warning for each up file without its down file. A missing base
// path is a warning here: Migrate reports it as an error when migrations are
// enabled. Validate fails only when the base path cannot be read. With
// Config.Validate false, it does nothing.
func (m *Migrator) Validate(ctx context.Context) error {
	if !m.config.Validate {
		return nil
	}
	info, err := os.Stat(m.config.BasePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		m.logger.WarnContext(ctx, "migrations base path does not exist", slog.String("base_path", m.config.BasePath))
		return nil
	case err != nil:
		return fmt.Errorf("stat base path %q: %w", m.config.BasePath, err)
	case !info.IsDir():
		m.logger.WarnContext(
			ctx,
			"migrations base path is not a directory",
			slog.String("base_path", m.config.BasePath),
		)
		return nil
	}
	entries, err := os.ReadDir(m.config.BasePath)
	if err != nil {
		return fmt.Errorf("read base path %q: %w", m.config.BasePath, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			m.validateModule(ctx, entry.Name())
		}
	}
	return nil
}

func (m *Migrator) validateModule(ctx context.Context, module string) {
	dir := filepath.Join(m.config.BasePath, module, "migrations")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	var ups []string
	downs := map[string]bool{}
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		switch name := d.Name(); {
		case d.IsDir():
		case strings.HasSuffix(name, ".up.sql"):
			ups = append(ups, name)
		case strings.HasSuffix(name, ".down.sql"):
			downs[name] = true
		}
		return nil
	})
	if err != nil {
		m.logger.ErrorContext(ctx, "failed to scan migrations directory", slog.String("module", module), log.Err(err))
		return
	}
	slices.Sort(ups)
	for _, up := range ups {
		down := strings.Replace(up, ".up.sql", ".down.sql", 1)
		if !downs[down] {
			m.logger.WarnContext(ctx, "missing corresponding down migration file",
				slog.String("module", module), slog.String("up_file", up), slog.String("missing_down", down))
		}
	}
}
