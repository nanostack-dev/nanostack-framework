package pipeline

import (
	"bytes"
	"fmt"
)

// diffLimit is the number of diff lines -diff prints per file.
const diffLimit = 20

// diffLines returns at most limit lines that show how have differs from
// want: a hunk header, then the lines of have that go ("-") and the lines of
// want that come ("+"), between the common prefix and suffix. It is a
// pointer to the change, not a minimal diff.
func diffLines(have, want []byte, limit int) []string {
	a, b := splitLines(have), splitLines(want)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	gone, come := a[pre:len(a)-suf], b[pre:len(b)-suf]
	out := []string{fmt.Sprintf("@@ -%d,%d +%d,%d @@", pre+1, len(gone), pre+1, len(come))}
	for _, l := range gone {
		out = append(out, "-"+l)
	}
	for _, l := range come {
		out = append(out, "+"+l)
	}
	if len(out) > limit {
		out = append(out[:limit-1], fmt.Sprintf("... %d more lines", len(out)-limit+1))
	}
	return out
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	lines := bytes.SplitAfter(b, []byte("\n"))
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(l) > 0 {
			out = append(out, string(bytes.TrimSuffix(l, []byte("\n"))))
		}
	}
	return out
}
