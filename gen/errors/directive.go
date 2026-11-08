package generrors

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"

	"github.com/nanostack-dev/nanostack-framework/gen"
)

// directive returns the arguments of the first doc-comment line that starts
// with prefix.
func directive(doc *ast.CommentGroup, prefix string) (string, bool) {
	if doc == nil {
		return "", false
	}
	for _, c := range doc.List {
		if rest, ok := strings.CutPrefix(c.Text, prefix); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// parseArgs reads key=value pairs. A value is either bare (no spaces) or a Go
// double-quoted string. Keys outside allowed are rejected.
func parseArgs(s string, allowed ...string) (map[string]string, *gen.Error) {
	out := map[string]string{}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		eq := strings.IndexByte(s, '=')
		if eq <= 0 || strings.ContainsAny(s[:eq], " \t\"") {
			return nil, problem(`write options as key=value, and quote a value with spaces: title="Not found"`,
				"expected key=value at %q", s)
		}
		key := s[:eq]
		val, rest, err := scanValue(key, s[eq+1:])
		if err != nil {
			return nil, err
		}
		s = rest
		if !slices.Contains(allowed, key) {
			return nil, problem("use one of the valid options: "+strings.Join(allowed, ", "), "unknown option %q", key)
		}
		if _, dup := out[key]; dup {
			return nil, problem("keep one of them", "option %s is set twice", key)
		}
		out[key] = val
	}
	return out, nil
}

// scanValue reads the value at the start of s, quoted or bare, and returns it
// with the rest of s.
func scanValue(key, s string) (string, string, *gen.Error) {
	if !strings.HasPrefix(s, `"`) {
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			end = len(s)
		}
		return s[:end], s[end:], nil
	}
	end := quotedEnd(s)
	if end < 0 {
		return "", "", problem(`close the value with a double quote`, "unterminated quoted value for %s", key)
	}
	v, err := strconv.Unquote(s[:end])
	if err != nil {
		return "", "", problem("write the value as a Go double-quoted string", "bad quoted value for %s: %v", key, err)
	}
	return v, s[end:], nil
}

// quotedEnd returns the index just past the closing quote of the Go string
// literal at the start of s, or -1.
func quotedEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}
