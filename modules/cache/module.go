package cache

import (
	"github.com/nanostack-dev/nanostack-framework/modules/config"

	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

var Module = fx.Module( //nolint:gochecknoglobals // Required for fx module definition.
	"cache",
	fx.Provide(func(configLoader config.Loader, logger zerolog.Logger) Cache {
		var cacheConfig Config
		logger.Info().Msg("loading cache configuration")
		if err := configLoader.LoadConfig("cache", &cacheConfig); err != nil {
			logger.Warn().Err(err).Msg("cache configuration not found; using no-op cache")
			return NewNoOpCache()
		}
		if cacheConfig.Address == "" {
			logger.Warn().Msg("cache address empty; using no-op cache")
			return NewNoOpCache()
		}
		redisCache, err := NewRedisCache(cacheConfig, logger.With().Str("component", "redis_cache").Logger())
		if err != nil {
			// Cache only when Redis is actually connected. Without a live shared
			// cache, fall back to no-op rather than a per-instance cache that
			// cannot be evicted cluster-wide (which leaves stale entries across
			// instances, e.g. an API key's permissions after an update).
			logger.Warn().Err(err).Msg("redis cache not connected; using no-op cache")
			return NewNoOpCache()
		}
		return redisCache
	}),
)
