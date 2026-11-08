// Package secret holds the framework's one type for sensitive values, such as
// API keys, passwords and tokens, and the one marker that every redacting path
// writes in their place.
//
// It also owns every rule that finds credentials outside a Secret: IsKey
// matches a name that looks like it holds one, RedactURL and RedactQuery hide
// the credentials of a URL, and RedactText hides what looks like a credential
// in free text. core/config binds settings of type Secret, prints URL
// settings through RedactURL and shows rejected values through RedactText.
// core/log writes Redacted for a Secret value and for every attribute whose
// key IsKey matches. The package imports only the standard library, so any
// layer can use it.
package secret

import (
	"fmt"
	"log/slog"
)

// Redacted is what every printing path writes instead of a sensitive value.
const Redacted = "[REDACTED]"

// Secret holds a sensitive value.
//
// Every printing path that calls its methods shows Redacted, or an empty
// string when the secret is unset: fmt with any verb (Format, String,
// GoString), slog (LogValue), encoding/json v1 and v2, YAML and any other
// encoder that honours encoding.TextMarshaler (MarshalText). Call Reveal at
// the single place that needs the real value.
//
// A Secret decodes from text (UnmarshalText), so JSON, environment and other
// text sources fill it directly.
//
// The value sits behind a pointer. fmt cannot call methods on a Secret held in
// an unexported struct field, so it prints the field by reflection; it then
// shows the pointer's address, never the value. Printers that follow pointers
// on purpose (spew, go-cmp reporters, debuggers) still reach it, so keep
// Secrets out of them.
//
// Two Secrets are == only when they are copies of one another; compare
// Reveal() to compare values.
type Secret struct {
	value *string
}

// New wraps value. An empty value gives an unset Secret.
func New(value string) Secret {
	if value == "" {
		return Secret{}
	}
	return Secret{value: &value}
}

// Reveal returns the secret value, or "" when it is unset. Keep calls next to
// the use that needs the value.
func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

// IsSet reports whether the secret has a non-empty value.
func (s Secret) IsSet() bool { return s.value != nil }

// String returns Redacted for a set secret and "" for an unset one.
func (s Secret) String() string {
	if !s.IsSet() {
		return ""
	}
	return Redacted
}

// GoString keeps %#v and explicit GoString calls redacted.
func (s Secret) GoString() string { return s.String() }

// Format makes every fmt verb, including %x and %q, print the redacted form.
func (s Secret) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

// LogValue keeps slog from printing the value.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalText keeps encoders (JSON v1 and v2, YAML, text) from printing the
// value.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText sets the secret from text. Empty text unsets it.
func (s *Secret) UnmarshalText(text []byte) error {
	*s = New(string(text))
	return nil
}
