// Package cors provides a config-driven browser-Origin allowlist so services
// never hardcode domains in source. An OriginPolicy combines an exact-match
// origin list with optional host suffixes (e.g. ".example.dev") matched against
// https origins — letting wildcard hostnames such as per-PR preview frontends be
// permitted through configuration rather than code.
//
// Typical wiring with github.com/go-chi/cors:
//
//	policy := cors.NewOriginPolicy(
//	    cors.ParseList(cfg.AllowedOrigin),          // exact origins (csv)
//	    cors.ParseList(cfg.AllowedOriginSuffixes),  // host suffixes (csv)
//	)
//	chicors.New(chicors.Options{
//	    AllowedOrigins:  policy.Exact(),
//	    AllowOriginFunc: func(_ *http.Request, origin string) bool { return policy.Allows(origin) },
//	})
//
// Prod typically configures only exact origins (no suffixes); dev/preview adds a
// zone suffix so dynamic preview hostnames are allowed without code changes.
package cors

import (
	"net/url"
	"strings"
)

// OriginPolicy decides whether a browser Origin header is allowed.
type OriginPolicy struct {
	exact    []string
	suffixes []string
}

// NewOriginPolicy builds a policy from an exact-match origin list and a list of
// host suffixes. A suffix may be written with or without a leading dot
// (".example.dev" or "example.dev"); the apex and any subdomain both match.
func NewOriginPolicy(exact, suffixes []string) OriginPolicy {
	return OriginPolicy{exact: exact, suffixes: suffixes}
}

// ParseList splits a comma-separated config value into trimmed, non-empty
// entries. A blank value yields an empty slice (deny by that mechanism).
func ParseList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Exact returns a copy of the configured exact-match origins, suitable for
// seeding a CORS middleware's static AllowedOrigins list.
func (p OriginPolicy) Exact() []string {
	return append([]string(nil), p.exact...)
}

// Allows reports whether origin is permitted: an exact match against the
// configured origins, or — for https origins only — a hostname that equals or
// is a subdomain of one of the configured suffixes.
func (p OriginPolicy) Allows(origin string) bool {
	for _, o := range p.exact {
		if origin == o {
			return true
		}
	}
	if len(p.suffixes) == 0 {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	for _, suffix := range p.suffixes {
		apex := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(suffix), "."))
		if apex == "" {
			continue
		}
		if host == apex || strings.HasSuffix(host, "."+apex) {
			return true
		}
	}
	return false
}
