package httpserver

import "testing"

func TestOptionsDefaults(t *testing.T) {
	options := (Options{}).withDefaults()
	if options.Port != defaultPort {
		t.Fatalf("expected default port %d, got %d", defaultPort, options.Port)
	}
	if len(options.AllowedMethods) == 0 {
		t.Fatal("expected default methods")
	}
}
