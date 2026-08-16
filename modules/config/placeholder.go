package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// errPlaceholderUnresolved reports that a placeholder named no environment
// variable that was set and carried no default. It is a sentinel rather than a
// message because the loader collects every unresolved name and reports them in
// one error, instead of failing on the first.
var errPlaceholderUnresolved = errors.New("no environment value and no default")

// placeholder is the parsed form of a single ${...} reference.
//
// The accepted syntax is:
//
//	${VAR}                 value of VAR
//	${VAR:default}         value of VAR, or default when VAR is unset
//	${file:VAR}            contents of the file whose path VAR holds
//	${file:VAR:default}    that file's contents, or default when VAR is unset
//
// Only the first colon separates a name from its default, so defaults may
// themselves contain colons (a URL, a host:port pair).
type placeholder struct {
	varName string
	// defaultValue is used verbatim when varName is unset. For a fromFile
	// placeholder it is still a literal value, never a second path to read.
	defaultValue string
	fromFile     bool
}

// parsePlaceholder splits the inside of a ${...} reference into its parts. It
// reads syntax only; nothing is looked up until resolve is called.
func parsePlaceholder(inner string) placeholder {
	var p placeholder
	if rest, found := strings.CutPrefix(inner, "file:"); found {
		p.fromFile = true
		inner = rest
	}
	p.varName, p.defaultValue, _ = strings.Cut(inner, ":")
	p.varName = strings.TrimSpace(p.varName)
	return p
}

// resolve returns the text the placeholder stands for. It reports
// errPlaceholderUnresolved when the variable is unset and no default applies,
// which the caller accumulates rather than treating as fatal on its own.
func (p placeholder) resolve() (string, error) {
	value, isSet := os.LookupEnv(p.varName)
	if !isSet {
		if p.defaultValue == "" {
			return "", errPlaceholderUnresolved
		}
		return p.defaultValue, nil
	}
	if !p.fromFile {
		return value, nil
	}
	contents, err := os.ReadFile(value)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file for %s from path %s: %w", p.varName, value, err)
	}
	return strings.TrimSpace(string(contents)), nil
}
