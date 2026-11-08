package config

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// Checks validate a parsed value and record an issue when it fails. The
// generator emits them from the validate struct tag and has already checked
// that the bounds and the default are valid for the field's type.

// Between checks lo <= v <= hi.
func Between[T cmp.Ordered](b *Binder, k Key, v, lo, hi T) {
	if v < lo || v > hi {
		b.invalid(k, fmt.Sprint(v), fmt.Sprintf("must be between %v and %v", lo, hi))
	}
}

// Min checks v >= lo.
func Min[T cmp.Ordered](b *Binder, k Key, v, lo T) {
	if v < lo {
		b.invalid(k, fmt.Sprint(v), fmt.Sprintf("must be at least %v", lo))
	}
}

// Max checks v <= hi.
func Max[T cmp.Ordered](b *Binder, k Key, v, hi T) {
	if v > hi {
		b.invalid(k, fmt.Sprint(v), fmt.Sprintf("must be at most %v", hi))
	}
}

// OneOf checks that v is one of allowed.
func OneOf[T comparable](b *Binder, k Key, v T, allowed ...T) {
	if slices.Contains(allowed, v) {
		return
	}
	names := make([]string, len(allowed))
	for i, a := range allowed {
		names[i] = fmt.Sprint(a)
	}
	b.invalid(k, fmt.Sprint(v), "must be one of: "+strings.Join(names, ", "))
}

// Scheme checks that u uses one of schemes.
func Scheme(b *Binder, k Key, u *url.URL, schemes ...string) {
	if u == nil || slices.Contains(schemes, u.Scheme) {
		return
	}
	b.invalid(k, secret.RedactURL(u), "must use scheme "+strings.Join(schemes, " or "))
}
