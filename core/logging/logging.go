package logging

import (
	"context"
	"io"
	"log/slog"

	"github.com/nanostack-dev/nanostack-framework/core/log"
	"github.com/nanostack-dev/nanostack-framework/core/nano"
)

// Module provides *slog.Logger and declares the []log.ContextAttrs collection.
var Module = nano.Module("logging", //nolint:gochecknoglobals // A nano module is a package-level value.
	nano.Providers(NewLogger),
	nano.Collect[log.ContextAttrs](),
)

// NewLogger validates s, builds the core/log handler with every attrs member
// and installs the logger as the slog default. The default matters: code
// without an injected logger, such as http/problem.Write, logs through
// slog.Default, and the standard library log package then writes through
// the same handler.
//
// An invalid LOG_LEVEL, LOG_FORMAT or LOG_WIRE is an error, so a typo fails
// the start instead of hiding debug output or breaking log parsing.
func NewLogger(s log.Settings, attrs []log.ContextAttrs) (*slog.Logger, error) {
	logger, err := newLogger(s, attrs, nil)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return logger, nil
}

// newLogger builds the logger. A nil out is os.Stdout.
func newLogger(s log.Settings, attrs []log.ContextAttrs, out io.Writer) (*slog.Logger, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	cfg.Output = out
	cfg.ContextAttrs = join(attrs)
	return log.New(cfg), nil
}

// join returns one ContextAttrs that calls each non-nil member in order, or
// nil when there is none, so core/log adds no context handler for nothing.
func join(members []log.ContextAttrs) log.ContextAttrs {
	var fns []log.ContextAttrs
	for _, fn := range members {
		if fn != nil {
			fns = append(fns, fn)
		}
	}
	switch len(fns) {
	case 0:
		return nil
	case 1:
		return fns[0]
	}
	return func(ctx context.Context) []slog.Attr {
		var out []slog.Attr
		for _, fn := range fns {
			out = append(out, fn(ctx)...)
		}
		return out
	}
}
