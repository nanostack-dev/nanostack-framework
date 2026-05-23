package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nanostack-dev/nanostack-framework/modules/config"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

func ProvideMigrationConfig(loader config.Loader) (MigrationConfig, error) {
	cfg := MigrationConfig{BasePath: "./migrations", Enabled: true, Validate: true}
	_ = loader.LoadConfig("migrations", &cfg)
	return cfg, nil
}

func ProvideModuleMigrator(log zerolog.Logger, db *sql.DB, config MigrationConfig) *MigrationProvider {
	return NewMigrationProvider(log, db, config)
}

func RegisterMigrationHooks(lifecycle fx.Lifecycle, log zerolog.Logger, migrator *MigrationProvider) {
	lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
		log.Info().Msg("initializing database migrations")
		if err := migrator.ValidateMigrations(); err != nil {
			return fmt.Errorf("migration validation failed: %w", err)
		}
		if err := migrator.RunMigrations(); err != nil {
			return fmt.Errorf("migration execution failed: %w", err)
		}
		return nil
	}})
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"migrations",
	fx.Provide(ProvideMigrationConfig, ProvideModuleMigrator),
	fx.Invoke(RegisterMigrationHooks),
)
