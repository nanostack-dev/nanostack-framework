package config

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// Parse helpers turn one raw value into a typed one. Their errors are short,
// user-facing sentences: the Binder adds the path, environment variable, value
// and origin.

// ParseString accepts any value.
func ParseString[T ~string](s string) (T, error) { return T(s), nil }

// ParseBool accepts true/false, 1/0, t/f in any case.
func ParseBool[T ~bool](s string) (T, error) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, errors.New("must be true or false")
	}
	return T(v), nil
}

// ParseInt accepts a base-10 integer that fits T.
func ParseInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64](s string) (T, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	if int64(T(n)) != n {
		return 0, errors.New("is out of range")
	}
	return T(n), nil
}

// ParseUint accepts a non-negative base-10 integer that fits T.
func ParseUint[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](s string) (T, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, errors.New("must be a non-negative integer")
	}
	if uint64(T(n)) != n {
		return 0, errors.New("is out of range")
	}
	return T(n), nil
}

// ParseFloat accepts a finite decimal number that fits T.
func ParseFloat[T ~float32 | ~float64](s string) (T, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("must be a number")
	}
	if math.IsInf(float64(T(f)), 0) {
		return 0, errors.New("is out of range")
	}
	return T(f), nil
}

// ParseDuration accepts a Go duration such as 300ms, 30s, 5m or 1h30m.
func ParseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, errors.New("must be a duration such as 300ms, 30s or 5m")
	}
	return d, nil
}

// ParseURL accepts an absolute URL with a scheme and a host.
//
// The issue for a value it rejects does not show the value: a URL setting
// often holds a DSN, and in a value that does not parse no rule can find the
// password reliably (a missing host, a space or a newline in the password, a
// libpq keyword string). A value it accepts that fails a check is shown
// through secret.RedactURL.
func ParseURL(s string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	// A host with an empty port ("app:", or ":") is how url.Parse reads a
	// password that is not percent-encoded, as in postgres://app:#pw@db.
	if err != nil || u.Scheme == "" || u.Host == "" || strings.HasSuffix(u.Host, ":") {
		return nil, errHiddenURL
	}
	return u, nil
}

// errHiddenURL is ParseURL's error. Its issue shows no value.
var errHiddenURL = &hiddenValueError{
	msg: "must be an absolute URL such as https://example.com; the value is not shown, as it may hold a password",
}

// hiddenValueError is a parse error whose issue must not show the rejected
// value.
type hiddenValueError struct{ msg string }

func (e *hiddenValueError) Error() string { return e.msg }

// ParseSecret accepts any value and wraps it in a secret.Secret.
func ParseSecret(s string) (secret.Secret, error) { return secret.New(s), nil }

// ParseText delegates to T's UnmarshalText, for types such as slog.Level or
// netip.Addr.
//
// Its error names the type, as in "must be a valid slog.Level", and drops the
// text of the UnmarshalText error: that text often repeats the raw value, and
// the issue already shows the value with what looks like a credential
// hidden. A DSN pasted into such a setting then shows only in that redacted
// form.
func ParseText[T any, P interface {
	*T
	encoding.TextUnmarshaler
}](s string) (T, error) {
	var v T
	if err := P(&v).UnmarshalText([]byte(s)); err != nil {
		return v, fmt.Errorf("must be a valid %T", v)
	}
	return v, nil
}
