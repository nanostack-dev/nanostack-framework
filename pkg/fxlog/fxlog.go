// Package fxlog adapts Uber Fx lifecycle events onto a zerolog logger, so an
// application's Fx startup/shutdown output is structured JSON like the rest of
// its logs instead of Fx's default plain-text console writer.
package fxlog

import (
	"strings"

	"github.com/rs/zerolog"
	"go.uber.org/fx/fxevent"
)

// Logger implements fxevent.Logger on top of zerolog.
//
// High-cardinality wiring events (provided, supplied, decorated, replaced,
// invoking, run) log at debug level to keep them out of noisy ingestion while
// staying available locally; lifecycle milestones (started, stopping) log at
// info; failures log at error.
type Logger struct {
	log zerolog.Logger
}

var _ fxevent.Logger = (*Logger)(nil)

// New returns an fxevent.Logger that writes to log.
func New(log zerolog.Logger) *Logger {
	return &Logger{log: log.With().Str("component", "fx").Logger()}
}

// LogEvent dispatches each Fx event to its handler. Branching per event lives in
// the handlers so this stays a flat dispatcher.
func (l *Logger) LogEvent(event fxevent.Event) {
	switch e := event.(type) {
	case *fxevent.OnStartExecuting:
		l.onHookExecuting("OnStart hook executing", e.FunctionName, e.CallerName)
	case *fxevent.OnStartExecuted:
		l.onHookExecuted("OnStart hook", e.FunctionName, e.CallerName, e.Runtime.String(), e.Err)
	case *fxevent.OnStopExecuting:
		l.onHookExecuting("OnStop hook executing", e.FunctionName, e.CallerName)
	case *fxevent.OnStopExecuted:
		l.onHookExecuted("OnStop hook", e.FunctionName, e.CallerName, e.Runtime.String(), e.Err)
	case *fxevent.Supplied:
		l.onSupplied(e)
	case *fxevent.Provided:
		l.onProvided(e)
	case *fxevent.Replaced:
		l.onReplaced(e)
	case *fxevent.Decorated:
		l.onDecorated(e)
	case *fxevent.Run:
		l.onRun(e)
	case *fxevent.Invoking:
		l.withModule(l.log.Debug(), e.ModuleName).Str("function", e.FunctionName).Msg("invoking")
	case *fxevent.Invoked:
		l.onInvoked(e)
	case *fxevent.Stopping:
		l.log.Info().Str("signal", strings.ToUpper(e.Signal.String())).Msg("received signal")
	case *fxevent.Stopped:
		l.logErr(e.Err, "stop failed")
	case *fxevent.RollingBack:
		l.log.Error().Err(e.StartErr).Msg("start failed, rolling back")
	case *fxevent.RolledBack:
		l.logErr(e.Err, "rollback failed")
	case *fxevent.Started:
		l.onStarted(e)
	case *fxevent.LoggerInitialized:
		l.onLoggerInitialized(e)
	}
}

func (l *Logger) onHookExecuting(msg, callee, caller string) {
	l.log.Debug().Str("callee", callee).Str("caller", caller).Msg(msg)
}

func (l *Logger) onHookExecuted(label, callee, caller, runtime string, err error) {
	if err != nil {
		l.log.Error().Err(err).Str("callee", callee).Str("caller", caller).Msg(label + " failed")
		return
	}
	l.log.Debug().Str("callee", callee).Str("caller", caller).Str("runtime", runtime).Msg(label + " executed")
}

func (l *Logger) onSupplied(e *fxevent.Supplied) {
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).
			Str("type", e.TypeName).Msg("error encountered while applying options")
		return
	}
	l.withModule(l.log.Debug(), e.ModuleName).Str("type", e.TypeName).Msg("supplied")
}

func (l *Logger) onProvided(e *fxevent.Provided) {
	for _, rtype := range e.OutputTypeNames {
		l.withModule(l.log.Debug(), e.ModuleName).
			Str("constructor", e.ConstructorName).
			Str("type", rtype).
			Bool("private", e.Private).
			Msg("provided")
	}
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).Msg("error encountered while applying options")
	}
}

func (l *Logger) onReplaced(e *fxevent.Replaced) {
	for _, rtype := range e.OutputTypeNames {
		l.withModule(l.log.Debug(), e.ModuleName).Str("type", rtype).Msg("replaced")
	}
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).Msg("error encountered while replacing")
	}
}

func (l *Logger) onDecorated(e *fxevent.Decorated) {
	for _, rtype := range e.OutputTypeNames {
		l.withModule(l.log.Debug(), e.ModuleName).
			Str("decorator", e.DecoratorName).Str("type", rtype).Msg("decorated")
	}
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).Msg("error encountered while applying options")
	}
}

func (l *Logger) onRun(e *fxevent.Run) {
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).
			Str("name", e.Name).Str("kind", e.Kind).Msg("error returned")
		return
	}
	l.withModule(l.log.Debug(), e.ModuleName).
		Str("name", e.Name).Str("kind", e.Kind).Str("runtime", e.Runtime.String()).Msg("run")
}

func (l *Logger) onInvoked(e *fxevent.Invoked) {
	if e.Err != nil {
		l.withModule(l.log.Error().Err(e.Err), e.ModuleName).
			Str("stack", e.Trace).Str("function", e.FunctionName).Msg("invoke failed")
		return
	}
	l.withModule(l.log.Debug(), e.ModuleName).Str("function", e.FunctionName).Msg("invoked")
}

func (l *Logger) onStarted(e *fxevent.Started) {
	if e.Err != nil {
		l.log.Error().Err(e.Err).Msg("start failed")
		return
	}
	l.log.Info().Msg("started")
}

func (l *Logger) onLoggerInitialized(e *fxevent.LoggerInitialized) {
	if e.Err != nil {
		l.log.Error().Err(e.Err).Msg("custom logger initialization failed")
		return
	}
	l.log.Debug().Str("function", e.ConstructorName).Msg("initialized custom fxevent.Logger")
}

// logErr logs msg at error level only when err is non-nil.
func (l *Logger) logErr(err error, msg string) {
	if err != nil {
		l.log.Error().Err(err).Msg(msg)
	}
}

// withModule appends the module name when present, mirroring Fx's own optional
// module field.
func (l *Logger) withModule(event *zerolog.Event, moduleName string) *zerolog.Event {
	if moduleName == "" {
		return event
	}
	return event.Str("module", moduleName)
}
