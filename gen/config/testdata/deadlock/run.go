//go:build !nanogen

package deadlock

import "context"

// Run calls the generated Build, as a service's run.go does.
func Run(ctx context.Context, cfg Config) error {
	_, err := Build(ctx, cfg, Overrides{})
	return err
}
