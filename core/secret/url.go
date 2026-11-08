package secret

import (
	"net/url"
	"strings"
)

// RedactURL returns u as text with every credential replaced by Redacted: the
// userinfo password and the value of each query parameter whose name IsKey
// matches, such as password=, sslpassword= or token=. It returns "" for nil.
//
// url.URL.Redacted hides only the userinfo password, and writes "xxxxx". A
// DSN such as postgres://db/app?password=pw shows its password there.
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.RawQuery = RedactQuery(u.RawQuery)
	if _, ok := u.User.Password(); !ok || c.Opaque != "" {
		return c.String()
	}
	// The marker must stay literal: url.UserPassword would percent-encode its
	// brackets. So write the URL without the password, then insert the marker
	// after the user name, which String writes right after the first "//".
	c.User = url.User(u.User.Username())
	s := c.String()
	user := c.User.String()
	i := strings.Index(s, "//") + len("//") + len(user)
	return s[:i] + ":" + Redacted + s[i:]
}

// RedactQuery returns a raw query string with the value of each parameter
// whose name IsKey matches replaced by Redacted. It keeps the order of the
// parameters and the encoding of everything else.
func RedactQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	params := strings.Split(rawQuery, "&")
	for i, param := range params {
		key, _, hasValue := strings.Cut(param, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		if hasValue && IsKey(name) {
			params[i] = key + "=" + Redacted
		}
	}
	return strings.Join(params, "&")
}

// RedactURLs applies RedactURL to every element.
func RedactURLs(us []*url.URL) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = RedactURL(u)
	}
	return out
}
