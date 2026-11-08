package nanoserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/nano"
	"github.com/nanostack-dev/nanostack-framework/http/nanoserver"
)

// checker is the method set of pkg/health.Checker. This package may not
// import pkg/health, so the test pins the shape instead.
type checker interface {
	Name() string
	Check(ctx context.Context) error
}

var _ checker = nanoserver.Readiness{}

func TestReadinessFollowsTheApp(t *testing.T) {
	var failing error
	app := nano.NewApp("readiness-test", nano.AppConfig{})
	app.SetLogger(discard())
	app.Health("postgres", func(context.Context) error { return failing })
	var ready checker = nanoserver.NewReadiness(app)

	if ready.Name() != "readiness" {
		t.Fatalf("Name() = %q, want readiness", ready.Name())
	}
	if err := ready.Check(t.Context()); err == nil {
		t.Fatal("Check passed before the App started")
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	if err := ready.Check(t.Context()); err != nil {
		t.Fatalf("Check on a running, healthy App = %v, want nil", err)
	}

	failing = errors.New("connection refused")
	if err := ready.Check(t.Context()); err == nil {
		t.Fatal("Check passed while a health check fails")
	}
}
