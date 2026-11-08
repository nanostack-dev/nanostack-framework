package secret

import (
	"net/url"
	"strings"
)

// RedactURL returns u as text with every credential replaced by Redacted: the
// userinfo password and the value of each query parameter whose name IsKey
// matches, such as password=, sslpassword= or token=. A URL that url.Parse
// probably misread, because its password was not percent-encoded, goes
// through RedactText as a whole. It returns "" for nil.
//
// url.URL.Redacted hides only the userinfo password, and writes "xxxxx". A
// DSN such as postgres://db/app?password=pw shows its password there.
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.RawQuery = RedactQuery(u.RawQuery)
	if c.Opaque != "" || misparsed(u) {
		return RedactText(c.String())
	}
	if _, ok := u.User.Password(); !ok {
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

// misparsed reports whether u looks like a URL whose password was not
// percent-encoded, so url.Parse found no userinfo: "app:#pw@db" reads as host
// "app:" with "pw@db" in the fragment, and "app:12/pw@db" as host "app:12"
// with "pw@db" in the path. An '@' in the query is an ordinary value, such as
// email=a@b.c, and does not count. An '@' in the path may be ordinary too
// (/npm/@org/pkg); such a URL is redacted more than needed, which is safe.
func misparsed(u *url.URL) bool {
	if u.User != nil {
		return false
	}
	return strings.HasSuffix(u.Host, ":") || strings.Contains(u.Path, "@") || strings.Contains(u.Fragment, "@")
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
