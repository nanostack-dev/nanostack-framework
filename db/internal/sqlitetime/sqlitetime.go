// Package sqlitetime holds the text forms of a time in a SQLite column, which keyset and
// searchexec share. SQLite has no time type: a time column holds text and compares as
// text, so a literal must have the text form of the stored values.
package sqlitetime

import (
	"fmt"
	"strings"
	"time"
)

// DriverLayout is the text form in which the SQLite driver writes a time.Time.
const DriverLayout = "2006-01-02 15:04:05.999999999-07:00"

// CheckLayout panics when layout does not sort in time order. The layout must start with
// "2006-01-02 15:04:05" or "2006-01-02T15:04:05". owner names the caller in the message.
// A layout comes from code, never from a request.
func CheckLayout(owner, layout string) {
	if !strings.HasPrefix(layout, "2006-01-02 15:04:05") && !strings.HasPrefix(layout, "2006-01-02T15:04:05") {
		panic(fmt.Sprintf("%s: SQLite layout %q does not sort in time order", owner, layout))
	}
}

// Step is the smallest time that layout writes: one second, or the unit of its last
// fraction digit ("05.000" and "05.999" write a millisecond). CheckLayout must accept
// layout.
func Step(layout string) time.Duration {
	digits := 0
	rest := layout[len("2006-01-02 15:04:05"):]
	if len(rest) > 1 && (rest[0] == '.' || rest[0] == ',') {
		for _, c := range rest[1:] {
			if c != '0' && c != '9' {
				break
			}
			digits++
		}
	}
	step := time.Second
	for range min(digits, 9) {
		step /= 10
	}
	return step
}
