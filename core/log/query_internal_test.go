package log

import (
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
)

// referenceQueryAttr is queryAttr as url.ParseQuery defines it: unescaped
// values, a pair with ';' or a bad escape left out, repeated names collected
// in order, names sorted.
func referenceQueryAttr(rawQuery string) slog.Attr {
	values, _ := url.ParseQuery(rawQuery)
	attrs := make([]slog.Attr, 0, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		switch v := values[name]; {
		case secret.IsKey(name):
			attrs = append(attrs, slog.String(name, secret.Redacted))
		case len(v) == 1:
			attrs = append(attrs, slog.String(name, v[0]))
		default:
			attrs = append(attrs, slog.Any(name, v))
		}
	}
	return slog.Attr{Key: KeyQuery, Value: slog.GroupValue(attrs...)}
}

func TestQueryAttrKeepsParseQuerySemantics(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"a=1", "b=2&a=1", "a=1&a=2&b=3&a=4", "a", "a=", "=v", "&&a=1&", "x=1;y=2&b=3",
		"bad=%zz&ok=1", "%zz=1&ok=2", "q=hello+world&e=a%20b", "token=t&Token=u&user%5Bpassword%5D=p",
		"z=1&y=2&x=3&w=4&v=5&u=6&t=7&s=8&r=9&q=10&p=11&o=12&n=13&m=14&l=15&k=16&j=17&a=0&j=18",
		strings.Repeat("p=1&", 10_001) + "q=2",
	} {
		got, want := queryAttr(raw), referenceQueryAttr(raw)
		if got.String() != want.String() {
			t.Errorf("queryAttr(%.60q)\n got  %v\n want %v", raw, got, want)
		}
	}
}
