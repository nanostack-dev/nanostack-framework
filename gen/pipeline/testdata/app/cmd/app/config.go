// Package app is the service of the pipeline fixture: a manifest, a Config
// and one provider that uses the API package and the error catalogue.
package app

import (
	"context"

	"example.com/pipelineapp/api"
	"example.com/pipelineapp/greeterr"
)

// Config is the service configuration.
type Config struct {
	Greeting GreetingConfig `config:"greeting"`
}

// GreetingConfig configures the greeter.
type GreetingConfig struct {
	// Greeting text.
	Text string `config:"text" default:"hello"`
}

// Greeter greets. It is a nano.Runner, so wire keeps it.
type Greeter struct{ text string }

// NewGreeter returns a Greeter.
func NewGreeter(cfg GreetingConfig) *Greeter { return &Greeter{text: cfg.Text} }

// Greet returns the greeting, or an error when it is empty.
func (g *Greeter) Greet(api.Body) (string, error) {
	if g.text == "" {
		return "", greeterr.GreetingEmpty()
	}
	return g.text, nil
}

// Run waits for the end of ctx.
func (g *Greeter) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
