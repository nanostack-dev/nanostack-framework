package secret

import (
	"regexp"
	"strings"
)

// textParam matches a key=value pair, as in a query string or a libpq keyword
// DSN. The value ends at white space, "&", ";" or ",", or is quoted with
// single or double quotes. A quote that is not closed runs to the end of the
// text, so an unbalanced quote hides too much, never too little.
var textParam = regexp.MustCompile(`([A-Za-z0-9_.\-]+)=('[^']*'?|"[^"]*"?|[^\s&;,]+)`)

// RedactText returns raw with what looks like a credential replaced by
// Redacted. It is for free text that may hold a credential in a form no
// parser accepts, such as a DSN pasted into the wrong config setting:
// core/config uses it for the rejected value an issue shows.
//
// It replaces:
//   - the text between the first ":" (after "://" when there is one) and the
//     last "@", where a userinfo password sits even when the text does not
//     parse as a URL, has spaces or lacks a scheme;
//   - the value of each key=value pair whose key IsKey matches, such as
//     password= in a query string or in "host=db password=pw", and of each
//     key that ends with "pass", such as pass= or db_pass=. IsKey leaves
//     "pass" out, because log keys such as "bypass" and "compass" are not
//     credentials; in rejected text, hiding too much is the safe error.
//
// It errs towards hiding too much: with several URLs in one text, the text
// between the first password and the last "@" is hidden. It is a guard, not
// a parser: prefer RedactURL for a parsed URL and Secret for a known
// credential.
func RedactText(raw string) string {
	s := raw
	if at := strings.LastIndexByte(s, '@'); at >= 0 {
		start := 0
		if i := strings.Index(s[:at], "://"); i >= 0 {
			start = i + len("://")
		}
		if colon := strings.IndexByte(s[start:at], ':'); colon >= 0 {
			s = s[:start+colon+1] + Redacted + s[at:]
		}
	}
	return textParam.ReplaceAllStringFunc(s, func(pair string) string {
		key, _, _ := strings.Cut(pair, "=")
		if IsKey(key) || hasFoldedSuffix(key, "pass") {
			return key + "=" + Redacted
		}
		return pair
	})
}
