package apisec

import "context"

type requirementsKey struct{}

// WithRequirements returns a context carrying the security requirements of the
// operation being served. Middleware attaches them once per request, before
// attempting any authentication, so everything downstream reads one resolution.
func WithRequirements(ctx context.Context, reqs Requirements) context.Context {
	return context.WithValue(ctx, requirementsKey{}, reqs)
}

// RequirementsFrom returns the requirements attached to ctx.
//
// ok is false when none were attached, meaning the request never passed through
// the middleware that resolves them. Callers must read that as "not authorised"
// rather than "no requirements", because an empty requirement set is a public
// operation — the two must not collapse into each other.
func RequirementsFrom(ctx context.Context) (Requirements, bool) {
	reqs, ok := ctx.Value(requirementsKey{}).(Requirements)
	return reqs, ok
}

// ScopesFromContext reports the scopes the operation demands of the named
// scheme, and whether the operation accepts that scheme at all. It returns
// false when no requirements are attached, so an unresolved request never looks
// permitted.
func ScopesFromContext(ctx context.Context, scheme string) ([]string, bool) {
	reqs, ok := RequirementsFrom(ctx)
	if !ok {
		return nil, false
	}
	return reqs.ScopesFor(scheme)
}
