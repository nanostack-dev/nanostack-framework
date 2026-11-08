package config

import (
	"strconv"
	"strings"
)

// Issue is one thing wrong with the configuration.
type Issue struct {
	// Path is the setting path, for example billing.due_days or
	// billing.hosts[2] for one list element.
	Path string
	// Env is the environment variable that sets this setting. It is empty for
	// an unknown key found in a file.
	Env string
	// Origin says where the bad value came from: "env BILLING_DUE_DAYS",
	// "file config/base.yaml", "default" or "secret store". It is empty for a
	// missing setting.
	Origin string
	// Message says what is wrong, for example "must be between 1 and 365".
	Message string
	// Value is the offending raw value. It is empty for secret settings.
	Value string
}

// String formats the issue on one line, for example:
//
//	billing.due_days (env BILLING_DUE_DAYS): must be between 1 and 365; got "0" from file config/base.yaml
func (p Issue) String() string {
	var b strings.Builder
	b.WriteString(p.Path)
	if p.Env != "" {
		b.WriteString(" (env ")
		b.WriteString(p.Env)
		b.WriteString(")")
	}
	b.WriteString(": ")
	b.WriteString(p.Message)
	switch {
	case p.Value != "" && p.Origin != "":
		b.WriteString("; got ")
		b.WriteString(strconv.Quote(p.Value))
		b.WriteString(" from ")
		b.WriteString(p.Origin)
	case p.Origin != "":
		b.WriteString("; from ")
		b.WriteString(p.Origin)
	}
	return b.String()
}

// Error is every issue found by one Bind. Bind never stops at the first
// issue, so one start-up attempt lists everything an operator must fix.
type Error struct {
	Issues []Issue
}

// Error lists every issue on one line, separated by "; ".
func (e *Error) Error() string {
	if len(e.Issues) == 1 {
		return "config: " + e.Issues[0].String()
	}
	parts := make([]string, len(e.Issues))
	for i, p := range e.Issues {
		parts[i] = p.String()
	}
	return "config: " + strconv.Itoa(len(e.Issues)) + " issues: " + strings.Join(parts, "; ")
}
