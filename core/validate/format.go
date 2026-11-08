package validate

import (
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// UUID reports whether s is an RFC 9562 UUID in its 36-character hyphenated
// form. Hex digits may be upper or lower case.
func UUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(c) {
				return false
			}
		}
	}
	return true
}

// Email reports whether s is an RFC 5321 mailbox: a dot-atom or quoted local
// part, "@", and a domain name or an address literal. Display names and
// comments are rejected.
func Email(s string) bool {
	at := strings.LastIndexByte(s, '@')
	if at <= 0 || at == len(s)-1 || len(s) > 254 {
		return false
	}
	local, domain := s[:at], s[at+1:]
	if len(local) > 64 || !validLocalPart(local) {
		return false
	}
	if strings.HasPrefix(domain, "[") && strings.HasSuffix(domain, "]") {
		lit := domain[1 : len(domain)-1]
		if v6, ok := strings.CutPrefix(lit, "IPv6:"); ok {
			return IPv6(v6)
		}
		return IPv4(lit)
	}
	return Hostname(domain) && strings.Contains(domain, ".")
}

func validLocalPart(s string) bool {
	if strings.HasPrefix(s, `"`) {
		return len(s) >= 2 && strings.HasSuffix(s, `"`) && validQuotedContent(s[1:len(s)-1])
	}
	return validDotAtom(s)
}

// validQuotedContent checks the inside of an RFC 5321 quoted string:
// printable ASCII, with " and \\ escaped.
func validQuotedContent(q string) bool {
	for i := 0; i < len(q); i++ {
		c := q[i]
		if c == '\\' {
			i++
			if i == len(q) || q[i] < 0x20 || q[i] > 0x7e {
				return false
			}
			continue
		}
		if c == '"' || c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// validDotAtom checks an RFC 5322 dot-atom: atext runs joined by dots.
func validDotAtom(s string) bool {
	if s[0] == '.' || s[len(s)-1] == '.' || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '.' && !isAtext(c) {
			return false
		}
	}
	return true
}

// isAtext reports whether c is an RFC 5322 atext character.
func isAtext(c byte) bool {
	return isAlnum(c) || strings.IndexByte("!#$%&'*+-/=?^_`{|}~", c) >= 0
}

// Hostname reports whether s is an RFC 1123 host name: dot-separated labels
// of letters, digits and inner hyphens, each at most 63 octets, 253 in all.
func Hostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			if c := label[i]; c != '-' && !isAlnum(c) {
				return false
			}
		}
	}
	return true
}

// URI reports whether s is an absolute RFC 3986 URI: a scheme followed by
// ":" and a hierarchical part, with no characters outside the URI grammar.
func URI(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || c >= 0x7f || strings.IndexByte(`"<>\^`+"`{|}", c) >= 0 {
			return false
		}
		if c == '%' && (i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2])) {
			return false
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" {
		return false
	}
	if !isAlpha(u.Scheme[0]) {
		return false
	}
	return true
}

// DateTime reports whether s is an RFC 3339 date-time, such as
// "2026-11-07T10:20:30.5Z" or "2026-11-07T10:20:30+01:00". A leap second
// (second 60) is accepted.
func DateTime(s string) bool {
	date, rest, ok := cutAny(s, "Tt")
	if !ok || !Date(date) || len(rest) < len("15:04:05Z") {
		return false
	}
	if !digits(rest[0:2]) || rest[2] != ':' || !digits(rest[3:5]) || rest[5] != ':' || !digits(rest[6:8]) {
		return false
	}
	hour, minute, second := atoi2(rest[0:2]), atoi2(rest[3:5]), atoi2(rest[6:8])
	if hour > 23 || minute > 59 || second > 60 {
		return false
	}
	rest = rest[8:]
	if strings.HasPrefix(rest, ".") {
		n := 1
		for n < len(rest) && isDigit(rest[n]) {
			n++
		}
		if n == 1 {
			return false
		}
		rest = rest[n:]
	}
	switch {
	case rest == "Z" || rest == "z":
		return true
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':' &&
		digits(rest[1:3]) && digits(rest[4:6]):
		return atoi2(rest[1:3]) <= 23 && atoi2(rest[4:6]) <= 59
	}
	return false
}

// Date reports whether s is an RFC 3339 full-date (YYYY-MM-DD) that exists in
// the proleptic Gregorian calendar.
func Date(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' || !digits(s[0:4]) || !digits(s[5:7]) || !digits(s[8:10]) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// IPv4 reports whether s is a dotted-quad IPv4 address without leading zeros.
func IPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// IPv6 reports whether s is an RFC 4291 IPv6 address without a zone.
func IPv6(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is6() && a.Zone() == ""
}

func cutAny(s, seps string) (before, after string, found bool) {
	if i := strings.IndexAny(s, seps); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

func atoi2(s string) int { return int(s[0]-'0')*10 + int(s[1]-'0') }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
func isAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }
func isHex(c byte) bool   { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }
