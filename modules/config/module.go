package config

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

func NewConfigLoaderProvider(_ fx.Lifecycle, log zerolog.Logger) (Loader, error) {
	loader := NewConfigLoader()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "application.yaml"
	}
	log.Info().Msg("initializing config loader")
	if err := loader.Init(configPath, "."); err != nil {
		log.Error().Err(err).Msg("failed to initialize config loader")
		return nil, fmt.Errorf("failed to initialize config loader: %w", err)
	}
	return loader, nil
}

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"config",
	fx.Provide(NewConfigLoaderProvider),
)
