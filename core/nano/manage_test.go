package nano

import (
	"context"
	"testing"
	"testing/synctest"
)

type everything struct{ rec *recorder }

func (e everything) Start(context.Context) error       { e.rec.add("start"); return nil }
func (e everything) Stop(context.Context) error        { e.rec.add("stop"); return nil }
func (e everything) CheckHealth(context.Context) error { return nil }
func (e everything) Run(ctx context.Context) error {
	e.rec.add("run")
	<-ctx.Done()
	return nil
}

func TestManageRegistersImplementedInterfaces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{})
		app.Manage("all", everything{&rec})
		app.Manage("nothing", struct{}{})
		app.Manage("nil", nil)
		if got := len(app.checks); got != 1 {
			t.Fatalf("checks = %d, want 1", got)
		}
		ctx, cancel := context.WithCancel(t.Context())
		errc := make(chan error, 1)
		go func() { errc <- app.Run(ctx) }()
		synctest.Wait()
		cancel()
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
		assertEvents(t, rec.get(), []string{"start", "run", "stop"})
	})
}

// server is a pointer component that an adapter provider also returns as an
// interface; the App must run and stop it once.
type server struct{ rec *recorder }

func (s *server) Stop(context.Context) error { s.rec.add("stop"); return nil }
func (s *server) Run(ctx context.Context) error {
	s.rec.add("run")
	<-ctx.Done()
	return nil
}

type handler interface{ Run(context.Context) error }

type unhashable struct {
	rec  *recorder
	tags []string
}

func (u unhashable) Stop(context.Context) error { u.rec.add("stop unhashable"); return nil }

func TestManageRegistersAComponentOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var rec recorder
		app := newTestApp(AppConfig{})
		srv := &server{&rec}
		app.Manage("*server", srv)
		var h handler = srv // what func AsHandler(s *server) handler { return s } returns
		app.Manage("handler", h)
		app.Manage("everything", everything{&rec})
		app.Manage("everything again", everything{&rec}) // an equal comparable value
		app.Manage("unhashable a", unhashable{&rec, nil})
		app.Manage("unhashable b", unhashable{&rec, nil}) // not comparable: kept
		if got := len(app.runners); got != 2 {
			t.Fatalf("runners = %d, want 2 (server and everything once each)", got)
		}

		ctx, cancel := context.WithCancel(t.Context())
		errc := make(chan error, 1)
		go func() { errc <- app.Run(ctx) }()
		synctest.Wait()
		cancel()
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
		got := rec.get()
		for _, e := range []string{"run", "stop"} {
			if n := count(got, e); n != 2 { // one from srv, one from everything
				t.Fatalf("%q happened %d times in %q, want 2", e, n, got)
			}
		}
		if n := count(got, "stop unhashable"); n != 2 {
			t.Fatalf("stop unhashable happened %d times, want 2", n)
		}
	})
}

func count(events []string, e string) int {
	n := 0
	for _, got := range events {
		if got == e {
			n++
		}
	}
	return n
}
