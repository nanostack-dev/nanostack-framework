package generrors

import (
	"strings"
	"unicode"
)

// initialisms are upper-cased when a parameter name starts with one, so id
// becomes ID and apiKey becomes APIKey, as Go style expects.
var initialisms = map[string]bool{
	"acl": true, "api": true, "ascii": true, "cpu": true, "css": true, "dns": true,
	"eof": true, "guid": true, "html": true, "http": true, "https": true, "id": true,
	"ip": true, "json": true, "lhs": true, "qps": true, "ram": true, "rhs": true,
	"rpc": true, "sla": true, "smtp": true, "sql": true, "ssh": true, "tcp": true,
	"tls": true, "ttl": true, "udp": true, "ui": true, "uid": true, "uuid": true,
	"uri": true, "url": true, "utf8": true, "vm": true, "xml": true, "xmpp": true,
	"xsrf": true, "xss": true,
}

// exportName turns a parameter name into an exported field name.
func exportName(name string) string {
	end := strings.IndexFunc(name, unicode.IsUpper)
	if end < 0 {
		end = len(name)
	}
	if first := name[:end]; initialisms[first] {
		return strings.ToUpper(first) + name[end:]
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// mixedInitialisms are initialisms written in mixed case, which the
// upper-case run rule below would split (IPv6 -> i_pv6).
var mixedInitialisms = []string{"OAuth", "IPv4", "IPv6", "GraphQL", "MySQL", "PostgreSQL"}

// snakeName turns a Go name into lower snake case, keeping initialisms
// together: InvoiceNotFound -> invoice_not_found, HTTPTimeout -> http_timeout,
// UserID -> user_id, IPv6Invalid -> ipv6_invalid, OAuth2Failed ->
// oauth2_failed. It only suggests a code; codes are always written out.
func snakeName(name string) string {
	r := []rune(name)
	var b strings.Builder
	for i := 0; i < len(r); i++ {
		c := r[i]
		if word, ok := mixedInitialismAt(r, i); ok {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteString(strings.ToLower(word))
			i += len([]rune(word)) - 1
			continue
		}
		if unicode.IsUpper(c) {
			prevLower := i > 0 && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]))
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if i > 0 && (prevLower || (nextLower && unicode.IsUpper(r[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(c))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// mixedInitialismAt reports the mixed-case initialism starting at r[i], if
// any. It must end the name or be followed by an upper-case letter or digit,
// so OAuthor is not split as OAuth + or.
func mixedInitialismAt(r []rune, i int) (string, bool) {
	for _, w := range mixedInitialisms {
		end := i + len([]rune(w))
		if end > len(r) || string(r[i:end]) != w {
			continue
		}
		if end == len(r) || unicode.IsUpper(r[end]) || unicode.IsDigit(r[end]) {
			return w, true
		}
	}
	return "", false
}
