package sqlitetime_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nanostack-dev/nanostack-framework/db/internal/sqlitetime"
)

func TestStep(t *testing.T) {
	for layout, want := range map[string]time.Duration{
		sqlitetime.DriverLayout:             time.Nanosecond,
		"2006-01-02 15:04:05":               time.Second,
		"2006-01-02T15:04:05Z07:00":         time.Second,
		"2006-01-02 15:04:05.000":           time.Millisecond,
		"2006-01-02T15:04:05.999Z07:00":     time.Millisecond,
		"2006-01-02 15:04:05,000000":        time.Microsecond,
		"2006-01-02 15:04:05.":              time.Second,
		"2006-01-02 15:04:05.999999999 MST": time.Nanosecond,
	} {
		assert.Equal(t, want, sqlitetime.Step(layout), layout)
	}
}

func TestCheckLayout(t *testing.T) {
	assert.NotPanics(t, func() { sqlitetime.CheckLayout("x", "2006-01-02 15:04:05") })
	assert.NotPanics(t, func() { sqlitetime.CheckLayout("x", "2006-01-02T15:04:05.000Z") })
	assert.PanicsWithValue(t, `x: SQLite layout "2006-01-02" does not sort in time order`, func() {
		sqlitetime.CheckLayout("x", "2006-01-02")
	})
}
