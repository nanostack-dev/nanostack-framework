package importset_test

import (
	"path"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/internal/importset"
)

func TestNamesNeverCollide(t *testing.T) {
	s := importset.New("example.com/self")
	s.Block("fault", "Kinds")
	if got := s.Add("example.com/core/fault", "fault"); got != "fault2" {
		t.Errorf("blocked name: got %s, want fault2", got)
	}
	if got := s.Reserve("time", "time"); got != "time" {
		t.Errorf("Reserve = %s", got)
	}
	if got := s.Add("example.com/time", "time"); got != "time2" {
		t.Errorf("reserved name: got %s, want time2", got)
	}
	if got := s.Add("gopkg.in/yaml.v3", "yaml"); got != "yaml" {
		t.Errorf("Add = %s", got)
	}
	if got := s.Add("example.com/core/fault", "other"); got != "fault2" {
		t.Errorf("second Add of a path must return its first name, got %s", got)
	}

	var b strings.Builder
	s.Write(&b)
	want := "import (\n" +
		"\tfault2 \"example.com/core/fault\"\n" +
		"\ttime2 \"example.com/time\"\n" +
		"\tyaml \"gopkg.in/yaml.v3\"\n" +
		")\n\n"
	if b.String() != want {
		t.Errorf("Write = %q\nwant    %q (time is reserved but unused, so not imported)", b.String(), want)
	}
}

func TestNoPredeclaredNames(t *testing.T) {
	s := importset.New("example.com/self")
	for p, want := range map[string]string{
		"example.com/any":   "any2",
		"example.com/error": "error2",
		"example.com/len":   "len2",
	} {
		if got := s.Add(p, path.Base(p)); got != want {
			t.Errorf("Add(%s) = %s, want %s", p, got, want)
		}
	}
}

func TestWriteWithoutImports(t *testing.T) {
	s := importset.New("example.com/self")
	s.Reserve("time", "time") // reserved, never added
	var b strings.Builder
	s.Write(&b)
	if b.Len() != 0 {
		t.Errorf("Write with no imports = %q, want nothing", b.String())
	}
}
