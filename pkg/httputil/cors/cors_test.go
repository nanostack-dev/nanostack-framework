package cors_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/httputil/cors"
)

func TestParseList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"blank", "   ", 0},
		{"single", "https://app.example.dev", 1},
		{"csv with spaces and blanks", "https://app.example.dev, , https://dev.example.dev,", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(cors.ParseList(tt.in)); got != tt.want {
				t.Fatalf("ParseList(%q) len = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestOriginPolicyAllows(t *testing.T) {
	policy := cors.NewOriginPolicy(
		[]string{"https://app.example.dev"},
		[]string{".example.dev"},
	)

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"exact match", "https://app.example.dev", true},
		{"subdomain via suffix", "https://pr-42.preview.example.dev", true},
		{"apex via suffix", "https://example.dev", true},
		{"http rejected for suffix match", "http://pr-42.preview.example.dev", false},
		{"unrelated host", "https://evil.com", false},
		{"lookalike suffix not matched", "https://notexample.dev", false},
		{"suffix-as-substring not matched", "https://example.dev.evil.com", false},
		{"garbage origin", "::::", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.Allows(tt.origin); got != tt.want {
				t.Fatalf("Allows(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestOriginPolicyNoSuffixesIsExactOnly(t *testing.T) {
	policy := cors.NewOriginPolicy([]string{"https://app.example.dev"}, nil)
	if policy.Allows("https://pr-1.preview.example.dev") {
		t.Fatal("no suffixes configured: subdomain must be rejected")
	}
	if !policy.Allows("https://app.example.dev") {
		t.Fatal("exact origin must still be allowed")
	}
}
