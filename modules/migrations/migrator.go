package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // Required for file source driver registration.
	"github.com/rs/zerolog"
)

type MigrationConfig struct {
	Enabled      bool   `yaml:"enabled"`
	BasePath     string `yaml:"basePath"`
	Validate     bool   `yaml:"validate"`
	AutoFixDirty bool   `yaml:"autoFixDirty"`
	ForceVersion uint   `yaml:"forceVersion"`
}

type MigrationProvider struct {
	log    zerolog.Logger
	db     *sql.DB
	config MigrationConfig
}

func NewMigrationProvider(log zerolog.Logger, db *sql.DB, config MigrationConfig) *MigrationProvider {
	return &MigrationProvider{log: log.With().Str("component", "migrations").Logger(), db: db, config: config}
}

func (m *MigrationProvider) RunMigrations() error {
	if !m.config.Enabled {
		m.log.Info().Msg("migrations are disabled")
		return nil
	}
	baseInfo, err := os.Stat(m.config.BasePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("migrations base path %q does not exist", m.config.BasePath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat migrations base path %q: %w", m.config.BasePath, err)
	}
	if !baseInfo.IsDir() {
		return fmt.Errorf("migrations base path %q is not a directory", m.config.BasePath)
	}

	driver, err := postgres.WithInstance(m.db, &postgres.Config{MigrationsTable: "schema_migrations"})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}
	migrator, err := migrate.NewWithDatabaseInstance("file://"+m.config.BasePath, "postgres", driver)
	if err != nil {
		if strings.Contains(err.Error(), "no migration files found") || errors.Is(err, os.ErrNotExist) {
			m.log.Info().Str("path", m.config.BasePath).Msg("no migration files found")
			return nil
		}
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	migrator.Log = &MigrateLogger{logger: m.log}

	version, dirty, versionErr := migrator.Version()
	if versionErr != nil && !errors.Is(versionErr, migrate.ErrNilVersion) {
		return fmt.Errorf("failed to get migration version: %w", versionErr)
	}
	if dirty {
		if err := m.handleDirtyState(migrator, version); err != nil {
			return err
		}
	}
	if m.config.ForceVersion > 0 {
		if m.config.ForceVersion > math.MaxInt {
			return fmt.Errorf("force version %d is too large", m.config.ForceVersion)
		}
		return migrator.Force(int(m.config.ForceVersion))
	}
	return m.executeMigrations(migrator)
}

func (m *MigrationProvider) handleDirtyState(migrator *migrate.Migrate, version uint) error {
	if !m.config.AutoFixDirty {
		return fmt.Errorf("dirty migration state detected at version %d", version)
	}
	if version > math.MaxInt {
		return fmt.Errorf("migration version %d is too large", version)
	}
	return migrator.Force(int(version))
}

func (m *MigrationProvider) executeMigrations(migrator *migrate.Migrate) error {
	if err := migrator.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			m.log.Info().Msg("no migration changes needed")
			return nil
		}
		return fmt.Errorf("migration failed: %w", err)
	}
	return nil
}

func (m *MigrationProvider) ValidateMigrations() error {
	if !m.config.Validate && !m.config.Enabled {
		return nil
	}
	baseInfo, err := os.Stat(m.config.BasePath)
	if os.IsNotExist(err) {
		m.log.Warn().Str("basePath", m.config.BasePath).Msg("migrations base path does not exist")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat migrations base path %q: %w", m.config.BasePath, err)
	}
	if !baseInfo.IsDir() {
		m.log.Warn().Str("basePath", m.config.BasePath).Msg("migrations base path is not a directory")
		return nil
	}
	moduleDirs, err := os.ReadDir(m.config.BasePath)
	if err != nil {
		return fmt.Errorf("failed to read migrations base path %q: %w", m.config.BasePath, err)
	}
	for _, dirEntry := range moduleDirs {
		if dirEntry.IsDir() {
			m.validateModuleMigrations(dirEntry.Name())
		}
	}
	return nil
}

func (m *MigrationProvider) validateModuleMigrations(moduleName string) {
	moduleMigrationsPath := filepath.Join(m.config.BasePath, moduleName, "migrations")
	if _, err := os.Stat(moduleMigrationsPath); err != nil {
		return
	}
	upFiles, downFiles := m.collectMigrationFiles(moduleName, moduleMigrationsPath)
	if upFiles == nil {
		return
	}
	m.validateMigrationPairs(moduleName, upFiles, downFiles)
}

func (m *MigrationProvider) collectMigrationFiles(moduleName, moduleMigrationsPath string) ([]string, []string) {
	var upFiles []string
	var downFiles []string
	if err := filepath.WalkDir(moduleMigrationsPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".sql") {
			if strings.HasSuffix(d.Name(), ".up.sql") {
				upFiles = append(upFiles, path)
			} else if strings.HasSuffix(d.Name(), ".down.sql") {
				downFiles = append(downFiles, path)
			}
		}
		return nil
	}); err != nil {
		m.log.Error().Err(err).Str("module", moduleName).Msg("failed to scan migrations directory")
		return nil, nil
	}
	sort.Strings(upFiles)
	sort.Strings(downFiles)
	return upFiles, downFiles
}

func (m *MigrationProvider) validateMigrationPairs(moduleName string, upFiles, downFiles []string) {
	for _, upFile := range upFiles {
		expectedDownFile := strings.Replace(filepath.Base(upFile), ".up.sql", ".down.sql", 1)
		found := false
		for _, downFile := range downFiles {
			if filepath.Base(downFile) == expectedDownFile {
				found = true
				break
			}
		}
		if !found {
			m.log.Warn().Str("module", moduleName).Str("upFile", filepath.Base(upFile)).Str("missingDown", expectedDownFile).Msg("missing corresponding down migration file")
		}
	}
}

type MigrateLogger struct {
	logger zerolog.Logger
}

func (l *MigrateLogger) Printf(format string, v ...interface{}) { l.logger.Debug().Msgf(format, v...) }

func (l *MigrateLogger) Verbose() bool { return true }
