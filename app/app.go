package app

import "go.uber.org/fx"

// Builder assembles an FX app while keeping raw fx.Option values visible.
type Builder struct {
	serviceName string
	options     []fx.Option
}

// New creates a service app builder.
func New(serviceName string) *Builder {
	return &Builder{serviceName: serviceName}
}

// ServiceName returns the logical service name for docs/logging conventions.
func (b *Builder) ServiceName() string {
	if b == nil {
		return ""
	}
	return b.serviceName
}

// With appends framework or infrastructure options.
func (b *Builder) With(options ...fx.Option) *Builder {
	if b == nil {
		return b
	}
	b.options = append(b.options, options...)
	return b
}

// Use appends application-owned feature options.
func (b *Builder) Use(options ...fx.Option) *Builder {
	return b.With(options...)
}

// Populate appends fx.Populate targets without hiding the FX primitive.
func (b *Builder) Populate(targets ...interface{}) *Builder {
	if len(targets) == 0 {
		return b
	}
	return b.With(fx.Populate(targets...))
}

// Options returns a copy of the assembled FX options.
func (b *Builder) Options() []fx.Option {
	if b == nil || len(b.options) == 0 {
		return nil
	}
	options := make([]fx.Option, len(b.options))
	copy(options, b.options)
	return options
}

// Build creates the underlying FX app.
func (b *Builder) Build() *fx.App {
	return fx.New(b.Options()...)
}

// Run builds and runs the underlying FX app.
func (b *Builder) Run() {
	b.Build().Run()
}
