package app

import "testing"

func TestBuilderOptionsAreCopied(t *testing.T) {
	builder := New("svc")
	options := builder.Options()
	if options != nil {
		t.Fatal("expected nil options before configuration")
	}
	if builder.ServiceName() != "svc" {
		t.Fatalf("unexpected service name %q", builder.ServiceName())
	}
}
