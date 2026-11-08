//go:build race

package genconfig

// raceEnabled passes -race on to the child go test of the golden fixture.
const raceEnabled = true
