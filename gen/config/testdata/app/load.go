package app

import (
	"context"

	nanoconf "github.com/nanostack-dev/nanostack-framework/core/config"
)

// Load is hand-written code that calls the generated Bind. It proves that
// regeneration works when the package depends on its own generated symbols.
func Load(ctx context.Context) (Config, error) {
	src, err := nanoconf.NewSources(nanoconf.Env(), nanoconf.OptionalFile("config/base.yaml"))
	if err != nil {
		return Config{}, err
	}
	cfg, err := Bind(ctx, src)
	if err != nil {
		return Config{}, err
	}
	_ = cfg.String()
	return cfg, nil
}
