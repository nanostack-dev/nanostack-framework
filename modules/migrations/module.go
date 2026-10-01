package migrations

import (
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

// RunBeforeStart migrates the database while the app is being built, before
// any OnStart hook. It is not an OnStart hook on purpose: fx cancels OnStart
// after its StartTimeout (15s by default) and the process then exits mid
// migration, leaving schema_migrations dirty although the SQL committed.
// Construction has no deadline, and every OnStart hook (HTTP server, workers)
// runs against the migrated schema.
func RunBeforeStart(log zerolog.Logger, migrator *MigrationProvider) error {
	log.Info().Msg("initializing database migrations")
	if err := migrator.ValidateMigrations(); err != nil {
		return fmt.Errorf("migration validation failed: %w", err)
	}
	if err := migrator.RunMigrations(); err != nil {
		return fmt.Errorf("migration execution failed: %w", err)
	}
	return nil
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"migrations",
	fx.Provide(ProvideMigrationConfig, ProvideModuleMigrator),
	fx.Invoke(RunBeforeStart),
)
