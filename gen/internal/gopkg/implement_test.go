package gopkg

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

// The go command's compile report of a dependency holds one error per line
// after its "# importpath" line; a type error is one error at its position.
func TestErrorLines(t *testing.T) {
	report := packages.Error{
		Kind: packages.ListError,
		Msg: "# example.com/app/api\n" +
			"../../api/check_api.go:6:45: cannot use &h{} (value of type *h) as CheckServer value in return " +
			"statement: *h does not implement CheckServer (missing method Check)\n" +
			"../../api/other.go:3:2: undefined: x\n" +
			"\t\thave Check()\n",
	}
	got := errorLines(report)
	if len(got) != 2 || got[0].file != "../../api/check_api.go" || got[0].line != 6 ||
		got[1].file != "../../api/other.go" || got[1].line != 3 || got[1].msg != "undefined: x" {
		t.Errorf("errorLines = %+v", got)
	}
	typeErr := packages.Error{Kind: packages.TypeError, Pos: "/m/api/a.go:9:2", Msg: "undefined: y"}
	if one := errorLines(typeErr); len(one) != 1 || one[0].file != "/m/api/a.go" || one[0].line != 9 {
		t.Errorf("errorLines(type error) = %+v", one)
	}
	if m := implementPattern.FindStringSubmatch(report.Msg); m == nil || m[1] != "*h" || m[2] != "CheckServer" ||
		m[3] != "missing method" || m[4] != "Check" {
		t.Errorf("implementPattern = %q", m)
	}
}
