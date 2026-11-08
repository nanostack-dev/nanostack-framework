package genvalidate

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ecmaSpace is ECMA-262 WhiteSpace and LineTerminator, which \s matches in
// JSON Schema patterns. RE2's \s is ASCII only and lacks \v.
const ecmaSpace = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// translatePattern converts an ECMA-262 regular expression, the dialect of
// JSON Schema's pattern, to an equivalent RE2 expression. It reads the
// pattern as ECMA-262 does with the u flag where the two modes differ (code
// points, \u{...}, \p{...}), and an escape that would carry an RE2 meaning
// ECMA-262 does not give it is an error rather than being passed on. Constructs RE2 cannot express (lookaround,
// backreferences) are errors too, reported at generation time rather than
// discovered at runtime.
func translatePattern(p string) (string, error) {
	t := &translator{p: p}
	for t.i = 0; t.i < len(p); t.i++ {
		if err := t.step(); err != nil {
			return "", fmt.Errorf("pattern %q: %w", p, err)
		}
	}
	out := t.b.String()
	if _, err := regexp.Compile(out); err != nil {
		return "", fmt.Errorf("pattern %q is not supported by RE2: %w", p, err)
	}
	return out, nil
}

// translator rewrites one pattern; i is the index of the current byte.
type translator struct {
	p       string
	i       int
	inClass bool
	b       strings.Builder
}

func (t *translator) step() error {
	c := t.p[t.i]
	switch {
	case c == '\\':
		return t.escape()
	case t.inClass && c == '[':
		// A literal in ECMA-262; RE2 would read [:digit:] as a POSIX class.
		t.b.WriteString(`\[`)
	case t.inClass:
		t.inClass = c != ']'
		t.b.WriteByte(c)
	case c == '[':
		return t.openClass()
	case c == '.':
		t.b.WriteString(`[^\n\r\x{2028}\x{2029}]`)
	case c == '(' && strings.HasPrefix(t.p[t.i:], "(?"):
		return t.group()
	default:
		t.b.WriteByte(c)
	}
	return nil
}

// group checks the "(?" forms: a non-capturing group and a named group are
// the same in both dialects. Lookaround cannot be expressed in RE2, and RE2's
// flag groups, (?i) and (?P<name>, are SyntaxErrors in ECMA-262.
func (t *translator) group() error {
	rest := t.p[t.i+2:]
	for _, look := range []string{"=", "!", "<=", "<!"} {
		if strings.HasPrefix(rest, look) {
			return errors.New("lookaround assertions are not supported by RE2")
		}
	}
	if !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "<") {
		return errors.New("only (?: and (?<name> groups are supported")
	}
	t.b.WriteByte('(')
	return nil
}

func (t *translator) openClass() error {
	rest := t.p[t.i:]
	switch {
	case strings.HasPrefix(rest, "[^]"):
		t.b.WriteString(`[\x00-\x{10FFFF}]`)
		t.i += 2
		return nil
	case strings.HasPrefix(rest, "[]"):
		return errors.New("the empty class [] is not supported")
	}
	t.inClass = true
	t.b.WriteByte('[')
	if strings.HasPrefix(rest[1:], "^") {
		t.b.WriteByte('^')
		t.i++
	}
	return nil
}

// escape translates the escape at t.i, a backslash.
//
// A backslash before ASCII punctuation stands for the character itself in
// ECMA-262 (u-mode allows it for the syntax characters, non-u for all) and in
// RE2, so it is kept. A backslash before a letter or digit must be an escape
// ECMA-262 defines: RE2 gives meanings of its own to \a, \z, \A, \Q, \pL and
// the like, where ECMA-262 reads a literal letter or a SyntaxError, so any
// other one is an error.
func (t *translator) escape() error {
	if t.i+1 == len(t.p) {
		return errors.New("ends with a backslash")
	}
	t.i++
	e := t.p[t.i]
	next := byte(0)
	if t.i+1 < len(t.p) {
		next = t.p[t.i+1]
	}
	switch {
	case e == 's' && t.inClass:
		t.b.WriteString(ecmaSpace)
	case e == 's':
		t.b.WriteString("[" + ecmaSpace + "]")
	case e == 'S' && t.inClass:
		return errors.New("\\S inside a character class is not supported")
	case e == 'S':
		t.b.WriteString("[^" + ecmaSpace + "]")
	case e == 'b' && t.inClass:
		t.b.WriteString(`\x08`) // backspace, as in ECMA-262
	case e == 'B' && t.inClass:
		return errors.New("\\B inside a character class is not valid ECMA-262")
	case strings.IndexByte("dDwWbBtnvfr", e) >= 0:
		t.b.WriteByte('\\')
		t.b.WriteByte(e)
	case e == 'x':
		if len(t.p) < t.i+3 || !allHex(t.p[t.i+1:t.i+3]) {
			return errors.New("\\x must be followed by two hex digits")
		}
		t.b.WriteString(`\x{` + t.p[t.i+1:t.i+3] + `}`)
		t.i += 2
	case e == 'u':
		return t.unicode()
	case e == 'c' && isASCIILetter(next):
		fmt.Fprintf(&t.b, `\x{%x}`, next%32)
		t.i++
	case e == '0' && !isASCIIDigit(next):
		t.b.WriteString(`\x00`)
	case e >= '1' && e <= '9' && !t.inClass:
		return errors.New("backreferences are not supported by RE2")
	case e == 'k' && next == '<':
		return errors.New("named backreferences are not supported by RE2")
	case e == 'p' || e == 'P':
		return t.property(e)
	case e == '/':
		t.b.WriteByte('/')
	case e < utf8.RuneSelf && !isASCIILetter(e) && !isASCIIDigit(e):
		t.b.WriteByte('\\')
		t.b.WriteByte(e)
	default:
		return fmt.Errorf("\\%c is not a valid ECMA-262 escape", e)
	}
	return nil
}

// unicode translates \uHHHH, a surrogate pair of two such escapes, or
// \u{H...}. A lone surrogate matches no character RE2 can see, so it is an
// error rather than a pattern that silently never matches.
func (t *translator) unicode() error {
	cp, n, err := unicodeEscape(t.p[t.i+1:])
	if err != nil {
		return err
	}
	braced := strings.HasPrefix(t.p[t.i+1:], "{")
	if !braced && isHighSurrogate(cp) {
		rest := t.p[t.i+1+n:]
		if lo, m, loErr := unicodeEscape(strings.TrimPrefix(rest, `\u`)); strings.HasPrefix(rest, `\u`) &&
			loErr == nil && !strings.HasPrefix(rest, `\u{`) && isLowSurrogate(lo) {
			cp = 0x10000 + (cp-0xD800)<<10 + (lo - 0xDC00)
			n += 2 + m
		}
	}
	switch {
	case isHighSurrogate(cp) || isLowSurrogate(cp):
		return fmt.Errorf("\\u%04X is a lone surrogate", cp)
	case cp > unicode.MaxRune:
		return errors.New("\\u{...} is beyond U+10FFFF")
	}
	fmt.Fprintf(&t.b, `\x{%X}`, cp)
	t.i += n
	return nil
}

// property translates \p{...} and \P{...}. RE2 names general categories
// and scripts by their value alone.
func (t *translator) property(e byte) error {
	rest := t.p[t.i+1:]
	end := strings.IndexByte(rest, '}')
	if !strings.HasPrefix(rest, "{") || end < 2 {
		return fmt.Errorf("\\%c must be followed by {name}", e)
	}
	name := rest[1:end]
	for _, prefix := range []string{"General_Category=", "gc=", "Script=", "sc="} {
		name = strings.TrimPrefix(name, prefix)
	}
	fmt.Fprintf(&t.b, `\%c{%s}`, e, name)
	t.i += end + 1
	return nil
}

// unicodeEscape reads the digits after \u: four hex digits or {H...}. n is
// the number of bytes read.
func unicodeEscape(s string) (cp rune, n int, err error) {
	digits := ""
	switch {
	case strings.HasPrefix(s, "{"):
		end := strings.IndexByte(s, '}')
		if end < 2 || end > 9 || !allHex(s[1:end]) {
			return 0, 0, errors.New("invalid \\u{...} escape")
		}
		digits, n = s[1:end], end+1
	case len(s) >= 4 && allHex(s[:4]):
		digits, n = s[:4], 4
	default:
		return 0, 0, errors.New("invalid \\u escape")
	}
	v, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		return 0, 0, errors.New("invalid \\u escape")
	}
	return rune(v), n, nil //nolint:gosec // G115: at most 8 hex digits, checked against MaxRune by the caller
}

func isHighSurrogate(r rune) bool { return 0xD800 <= r && r <= 0xDBFF }
func isLowSurrogate(r rune) bool  { return 0xDC00 <= r && r <= 0xDFFF }

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isASCIIDigit(c) && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isASCIIDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isASCIILetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
