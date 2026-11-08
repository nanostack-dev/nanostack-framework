package nano

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	nanolog "github.com/nanostack-dev/nanostack-framework/core/log"
)

// Default timeouts used when an AppConfig field is zero.
const (
	DefaultStartTimeout  = 15 * time.Second
	DefaultStopTimeout   = 15 * time.Second
	DefaultHealthTimeout = 5 * time.Second
)

// AppConfig holds the App's timeouts. A service embeds it in its Config as a
// section, App nano.AppConfig `config:"app"`, so nano gen config binds it from
// APP_START_TIMEOUT, APP_STOP_TIMEOUT and APP_HEALTH_TIMEOUT (or the app
// section of a YAML layer) with the defaults below, and the generated Build
// passes it to NewApp. A zero field of an AppConfig built by hand also takes
// the default.
//
// The stop bound of a process is StopTimeout plus StopGrace for each Stopper
// called after StopTimeout has passed; DrainDelay is counted inside
// StopTimeout. Set the orchestrator's grace period (Docker's
// stop_grace_period, Kubernetes' terminationGracePeriodSeconds) above it, or
// the runtime kills the process before Stop ends. Docker's default is 10s,
// below the default StopTimeout.
type AppConfig struct {
	// StartTimeout bounds the whole start phase: every Starter together.
	StartTimeout time.Duration `config:"start_timeout" default:"15s" json:"start_timeout" yaml:"start_timeout"`
	// StopTimeout bounds the stop phase: draining the Runners and every
	// Stopper share it. A Stopper called after it has passed still gets
	// StopGrace, so a slow Runner or Stopper never skips a later Stopper.
	StopTimeout time.Duration `config:"stop_timeout" default:"15s" json:"stop_timeout" yaml:"stop_timeout"`
	// HealthTimeout bounds one readiness evaluation, all checks together.
	HealthTimeout time.Duration `config:"health_timeout" default:"5s" json:"health_timeout" yaml:"health_timeout"`
	// DrainDelay is how long Stop keeps the Runners serving after readiness
	// turns unavailable, so a load balancer stops routing new requests to
	// the instance before its listeners close. Set it to at least the load
	// balancer's discovery refresh plus its health-check interval (about 10s
	// behind Traefik with a 5s refresh and a 5s check; periodSeconds ×
	// failureThreshold plus a margin on Kubernetes). It is counted inside
	// StopTimeout and must leave at least StopGrace of it for the Runners:
	// Start fails when DrainDelay is above StopTimeout minus StopGrace.
	// That check only guarantees StopGrace: nanoserver's shutdown_timeout
	// must fit in StopTimeout minus DrainDelay.
	// Zero, the default, cancels the Runners at once.
	DrainDelay time.Duration `config:"drain_delay" default:"0s" json:"drain_delay" yaml:"drain_delay"`
}

func (c AppConfig) withDefaults() AppConfig {
	if c.StartTimeout <= 0 {
		c.StartTimeout = DefaultStartTimeout
	}
	if c.StopTimeout <= 0 {
		c.StopTimeout = DefaultStopTimeout
	}
	if c.HealthTimeout <= 0 {
		c.HealthTimeout = DefaultHealthTimeout
	}
	return c
}

// check reports a configuration that Stop cannot honour: a DrainDelay that
// leaves the Runners less than StopGrace of StopTimeout to return. Clamping
// it instead would cut the drain short in silence, and a load balancer would
// still route to the instance while its listeners close.
func (c AppConfig) check() error {
	if c.DrainDelay > 0 && c.StopTimeout-c.DrainDelay < StopGrace {
		return fmt.Errorf("drain_delay %s leaves less than %s of stop_timeout %s for the Runners to return; "+
			"raise stop_timeout to at least drain_delay + %s", c.DrainDelay, StopGrace, c.StopTimeout, StopGrace)
	}
	return nil
}

type state int

const (
	stateNew state = iota
	stateStarting
	stateRunning
	stateStopping
	stateStopped
)

func (s state) String() string {
	switch s {
	case stateNew:
		return "new"
	case stateStarting:
		return "starting"
	case stateRunning:
		return "running"
	case stateStopping:
		return "stopping"
	default:
		return "stopped"
	}
}

// hook is one component's start and stop pair. A hook without start is
// active from registration: it guards a resource its constructor acquired.
// A hook with start becomes active only when start succeeds.
type hook struct {
	name   string
	start  func(context.Context) error
	stop   func(context.Context) error
	active bool
}

type runner struct {
	name string
	run  func(context.Context) error
}

type check struct {
	name  string
	check func(context.Context) error
}

// App owns a service's components. Components are registered in dependency
// order (the generated Build does this); Start runs Starters in that order,
// Runners then run concurrently under one cancellable context, and Stop
// cancels the Runners and stops the components in reverse order.
//
// An App runs once: after Stop, it cannot be started again.
type App struct {
	name string
	cfg  AppConfig

	mu      sync.Mutex
	log     *slog.Logger // nil: slog.Default() at the time of each line
	state   state
	hooks   []*hook
	runners []runner
	checks  []check

	cancelRun context.CancelFunc
	running   map[string]int // runner name -> live goroutines
	runWG     sync.WaitGroup
	runErrs   []error
	exited    chan struct{} // closed when the first Runner returns
	exitOnce  sync.Once
	stopping  chan struct{} // closed when Stop begins

	stopOnce sync.Once
	stopErr  error

	managed []any // values registered through Manage, for identity dedupe

	checkMu    sync.Mutex
	lastChecks map[string]string // check key -> its last error, "" when it passed
}

// ErrShutdownDuringStart reports that ctx was cancelled (usually by SIGINT or
// SIGTERM), or Stop was called, while the App was starting. Start then stops
// what it started. It is a requested shutdown, not a failure: Main exits with
// ExitOK when nothing else failed.
var ErrShutdownDuringStart = errors.New("nano: shutdown requested while starting")

// NewApp returns an App named name. Zero fields in cfg take the defaults. A
// DrainDelay above StopTimeout minus StopGrace makes Start fail.
func NewApp(name string, cfg AppConfig) *App {
	return &App{
		name:     name,
		cfg:      cfg.withDefaults(),
		running:  map[string]int{},
		exited:   make(chan struct{}),
		stopping: make(chan struct{}),
	}
}

// Name returns the App's name.
func (a *App) Name() string { return a.name }

// SetLogger replaces the logger the App writes lifecycle events to. The
// default is slog.Default() at the time of each line, so a logger the service
// installs with slog.SetDefault after NewApp, as the generated Build's
// providers do, receives every line at its own level. Lifecycle lines carry no service name of their
// own: unless the handler is core/log's with Config.Service set, pass a
// logger that carries it, such as logger.With(log.KeyService, "billing").
func (a *App) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log = logger
}

func (a *App) logger() *slog.Logger {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.log == nil {
		return slog.Default()
	}
	return a.log
}

// Hook registers a component with an optional start and an optional stop
// function. Components start in registration order and stop in reverse.
// A stop function runs only when its start succeeded, or when there is no
// start: then it releases what the constructor acquired, and it runs even if
// the App never starts.
func (a *App) Hook(name string, start, stop func(context.Context) error) {
	if start == nil && stop == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mustBeNew("Hook")
	a.hooks = append(a.hooks, &hook{name: name, start: start, stop: stop, active: start == nil})
}

// OnStart registers a start function: Hook(name, start, nil).
func (a *App) OnStart(name string, start func(context.Context) error) { a.Hook(name, start, nil) }

// OnStop registers a stop function: Hook(name, nil, stop).
func (a *App) OnStop(name string, stop func(context.Context) error) { a.Hook(name, nil, stop) }

// OnCleanup registers a cleanup function returned by a provider, as a stop
// function that cannot fail.
func (a *App) OnCleanup(name string, cleanup func()) {
	if cleanup == nil {
		return
	}
	a.OnStop(name, func(context.Context) error { cleanup(); return nil })
}

// OnRun registers a long-running function. It runs after every Starter
// succeeded, under a context that Stop cancels. See Runner for the contract.
func (a *App) OnRun(name string, run func(context.Context) error) {
	if run == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mustBeNew("OnRun")
	a.runners = append(a.runners, runner{name: name, run: run})
}

// Health registers a readiness check. See HealthChecker.
func (a *App) Health(name string, fn func(context.Context) error) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mustBeNew("Health")
	a.checks = append(a.checks, check{name: name, check: fn})
}

// Manage registers whichever lifecycle interfaces v implements: Starter and
// Stopper as one Hook, Runner with OnRun and HealthChecker with Health. The
// generated Build registers every component this way.
//
// Manage registers a component once. A value already passed to Manage (the
// same pointer, or an equal comparable value) is skipped, so a provider that
// returns one of its dependencies as an interface, or an override set on two
// fields, does not run or stop that component twice.
func (a *App) Manage(name string, v any) {
	if v == nil || !a.track(v) {
		return
	}
	var start, stop func(context.Context) error
	if s, ok := v.(Starter); ok {
		start = s.Start
	}
	if s, ok := v.(Stopper); ok {
		stop = s.Stop
	}
	a.Hook(name, start, stop)
	if r, ok := v.(Runner); ok {
		a.OnRun(name, r.Run)
	}
	if h, ok := v.(HealthChecker); ok {
		a.Health(name, h.CheckHealth)
	}
}

// track records v as managed and reports whether it was new.
func (a *App) track(v any) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mustBeNew("Manage")
	for _, seen := range a.managed {
		if sameValue(seen, v) {
			return false
		}
	}
	a.managed = append(a.managed, v)
	return true
}

// sameValue reports whether a and b are equal. Values of one non-comparable
// dynamic type (a struct holding a slice, say) are never equal.
func sameValue(a, b any) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

func (a *App) mustBeNew(method string) {
	if a.state != stateNew {
		panic(fmt.Sprintf("nano: App.%s called on a %s app; register components before Start", method, a.state))
	}
}

// Run starts the App, waits until ctx is done or a Runner returns, then
// stops the App. It returns nil after a clean shutdown triggered by ctx, and
// otherwise the joined start, Runner and stop errors. When ctx is cancelled
// during Start, the error wraps ErrShutdownDuringStart.
func (a *App) Run(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		a.logger().InfoContext(ctx, "nano: shutdown requested", slog.Any(nanolog.KeyCause, context.Cause(ctx)))
	case <-a.exited:
	case <-a.stopping: // someone called Stop directly
	}
	stopErr := a.Stop(ctx)
	return errors.Join(a.runnerErrors(), stopErr)
}

// Start runs every Starter in registration order under the start timeout,
// then launches the Runners. If a Starter fails, Start stops the active
// components in reverse order and returns the joined errors; the App is then
// stopped and cannot be started again.
//
// If ctx is cancelled, or Stop is called, before every Starter returned,
// Start starts nothing more, stops what it started and returns an error
// wrapping ErrShutdownDuringStart, joined with any stop error.
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.state != stateNew {
		st := a.state
		a.mu.Unlock()
		return fmt.Errorf("nano: start %s: app is %s", a.name, st)
	}
	if err := a.cfg.check(); err != nil {
		a.mu.Unlock()
		err = fmt.Errorf("nano: start %s: %w", a.name, err)
		// Main logs no start error itself: without this line the process
		// would exit 1 with no reason.
		a.logger().ErrorContext(ctx, "nano: start failed", nanolog.Err(err))
		// Stop releases what the constructors acquired.
		return errors.Join(err, a.Stop(ctx))
	}
	a.state = stateStarting
	hooks := slices.Clone(a.hooks)
	a.mu.Unlock()

	logger := a.logger()
	logger.InfoContext(ctx, "nano: starting",
		slog.Int("components", len(hooks)), slog.Int("runners", len(a.runners)))
	began := time.Now()

	startCtx, cancel := context.WithTimeout(ctx, a.cfg.StartTimeout)
	defer cancel()
	for _, h := range hooks {
		if h.start == nil {
			continue
		}
		if err := a.startHook(ctx, startCtx, h); err != nil {
			return err
		}
	}

	a.mu.Lock()
	if a.state != stateStarting { // Stop ran concurrently.
		a.mu.Unlock()
		return fmt.Errorf("nano: start %s: %w", a.name, ErrShutdownDuringStart)
	}
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	a.cancelRun = cancelRun
	a.state = stateRunning
	for _, r := range a.runners {
		a.running[r.name]++
		a.runWG.Add(1)
		go a.runOne(runCtx, r)
	}
	a.mu.Unlock()
	logger.InfoContext(ctx, "nano: started", slog.Duration("took", time.Since(began)))
	return nil
}

// startHook runs one Starter under startCtx. On failure it stops the App and
// returns the error Start returns.
func (a *App) startHook(ctx, startCtx context.Context, h *hook) error {
	if ctx.Err() != nil {
		return a.shutdownDuringStart(ctx, h.name, context.Cause(ctx))
	}
	logger := a.logger()
	t := time.Now()
	if err := a.callStarter(ctx, startCtx, h); err != nil {
		if ctx.Err() != nil && (nanolog.IsContextError(err) || errors.Is(err, context.Cause(ctx))) {
			return a.shutdownDuringStart(ctx, h.name, err) // the shutdown interrupted this Starter
		}
		err = fmt.Errorf("nano: start %s: %w", h.name, err)
		logger.ErrorContext(ctx, "nano: start failed", nanolog.Err(err), slog.String(nanolog.KeyComponent, h.name))
		if ctx.Err() != nil { // a real failure that a shutdown request met
			err = errors.Join(err, fmt.Errorf("nano: start %s: %w", a.name, ErrShutdownDuringStart))
		}
		return errors.Join(err, a.Stop(ctx))
	}
	a.mu.Lock()
	stopped := a.state != stateStarting
	h.active = !stopped
	a.mu.Unlock()
	if stopped { // Stop ran concurrently and did not see this component.
		var stopErr error
		if h.stop != nil {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.StopTimeout)
			if stopErr = call(stopCtx, h.stop); stopErr != nil {
				stopErr = fmt.Errorf("nano: stop %s: %w", h.name, stopErr)
				logger.ErrorContext(ctx, "nano: stop failed", nanolog.Err(stopErr),
					slog.String(nanolog.KeyComponent, h.name))
			}
			cancel()
		}
		return errors.Join(fmt.Errorf("nano: start %s: %w", a.name, ErrShutdownDuringStart), stopErr)
	}
	logger.DebugContext(
		ctx,
		"nano: started",
		slog.String(nanolog.KeyComponent, h.name),
		slog.Duration("took", time.Since(t)),
	)
	return nil
}

// callStarter runs the Starter of h under startCtx. When a shutdown
// interrupts it, callStarter waits for it to return, within what is left of
// StartTimeout: the Starter was handed the cancellation and is expected to
// unwind, and Stop must not stop its dependencies under it. A Starter that
// still succeeds is marked active, so Stop stops it in reverse order, and
// callStarter returns the interruption. A Starter abandoned at the deadline
// is watched: when it succeeds late, the watcher stops it.
func (a *App) callStarter(ctx, startCtx context.Context, h *hook) error {
	pending, err := callTracked(startCtx, h.start)
	if pending == nil {
		return err
	}
	if ctx.Err() != nil {
		if deadline, ok := startCtx.Deadline(); ok {
			timer := time.NewTimer(time.Until(deadline))
			defer timer.Stop()
			select {
			case late := <-pending:
				if late == nil {
					a.mu.Lock()
					h.active = a.state == stateStarting
					a.mu.Unlock()
					return err
				}
				return late
			case <-timer.C:
			}
		}
	}
	go a.watchAbandoned(context.WithoutCancel(ctx), h, pending)
	return err
}

// watchAbandoned waits for the result of a Starter the App gave up on, logs
// it, and stops the component when the Starter succeeded.
func (a *App) watchAbandoned(ctx context.Context, h *hook, pending <-chan error) {
	logger := a.logger().With(slog.String(nanolog.KeyComponent, h.name))
	err := <-pending
	if err != nil {
		logger.WarnContext(ctx, "nano: abandoned starter returned", nanolog.Err(err))
		return
	}
	logger.WarnContext(ctx, "nano: abandoned starter succeeded late; stopping it")
	if h.stop == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, StopGrace)
	defer cancel()
	if stopErr := call(stopCtx, h.stop); stopErr != nil {
		logger.ErrorContext(ctx, "nano: stop failed", nanolog.Err(fmt.Errorf("nano: stop %s: %w", h.name, stopErr)))
	}
}

// shutdownDuringStart stops what started after a shutdown request reached
// Start at component: before its Starter ran, or while the Starter ran and
// returned the cancellation. A Starter that fails for another reason does not
// come here: startHook logs and returns its error.
func (a *App) shutdownDuringStart(ctx context.Context, component string, cause error) error {
	a.logger().InfoContext(ctx, "nano: shutdown requested while starting",
		slog.String(nanolog.KeyComponent, component), slog.Any(nanolog.KeyCause, cause))
	err := fmt.Errorf("nano: start %s: at %s: %w", a.name, component, ErrShutdownDuringStart)
	return errors.Join(err, a.Stop(ctx))
}

func (a *App) runOne(ctx context.Context, r runner) {
	defer a.runWG.Done()
	err := runGuarded(ctx, r.run)

	a.mu.Lock()
	a.running[r.name]--
	if a.running[r.name] == 0 {
		delete(a.running, r.name)
	}
	shuttingDown := a.state >= stateStopping
	switch {
	case shuttingDown && (err == nil || errors.Is(err, context.Canceled)):
		err = nil
	case err == nil:
		err = fmt.Errorf("nano: runner %s returned before shutdown", r.name)
	default:
		err = fmt.Errorf("nano: runner %s: %w", r.name, err)
	}
	if err != nil {
		a.runErrs = append(a.runErrs, err)
	}
	a.mu.Unlock()

	if err != nil {
		a.logger().ErrorContext(ctx, "nano: runner failed", nanolog.Err(err), slog.String(nanolog.KeyComponent, r.name))
	}
	if !shuttingDown {
		a.exitOnce.Do(func() { close(a.exited) })
	}
}

func (a *App) runnerErrors() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return errors.Join(a.runErrs...)
}
