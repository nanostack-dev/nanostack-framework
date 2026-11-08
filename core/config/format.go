package config

import (
	"log/slog"
	"strconv"
	"strings"
)

// Format renders a LogValue group as space-separated path=value pairs in
// declaration order, for example "billing.due_days=30 billing.stripe_key=[REDACTED]".
// Generated String methods call it on the redacted LogValue, so String and
// logs always agree and neither shows a secret.
func Format(v slog.Value) string {
	var b strings.Builder
	formatInto(&b, "", v)
	return b.String()
}

func formatInto(b *strings.Builder, prefix string, v slog.Value) {
	v = v.Resolve()
	if v.Kind() != slog.KindGroup {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(prefix)
		b.WriteByte('=')
		s := v.String()
		if s == "" || strings.ContainsAny(s, " \t\n\"=") {
			s = strconv.Quote(s)
		}
		b.WriteString(s)
		return
	}
	for _, a := range v.Group() {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		formatInto(b, key, a.Value)
	}
}
