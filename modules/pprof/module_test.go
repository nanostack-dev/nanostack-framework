package pprof

import "testing"

func TestOptionsDefaults(t *testing.T) {
	options := (Options{}).withDefaults()
	if options.Addr != defaultAddr {
		t.Fatalf("expected default addr %q, got %q", defaultAddr, options.Addr)
	}
	if options.EnableEnv != defaultEnv {
		t.Fatalf("expected default env %q, got %q", defaultEnv, options.EnableEnv)
	}
}
