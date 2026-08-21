package functional_test

import (
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

// The benchmarks below compare a chained Seq against the hand-written loop it
// replaces. Run one implementation per process and interleave the repetitions:
// several implementations in one process contaminate each other through the
// heap, and blocked repetitions let background drift land on one implementation
// alone. BenchmarkLoopFilterMapControl is a copy of BenchmarkLoopFilterMap and
// exists only to report the noise floor of a run.

const benchSize = 1000

type benchItem struct {
	ID     int
	Score  int
	Active bool
}

type benchResult struct {
	ID      int
	Doubled int
}

var (
	benchSinkResult []benchResult
	benchSinkInt    int
)

func benchItems() []benchItem {
	out := make([]benchItem, benchSize)
	for i := range out {
		out[i] = benchItem{ID: i, Score: i % 97, Active: i%2 == 0}
	}
	return out
}

func benchActive(item benchItem) bool { return item.Active }

func benchMap(item benchItem) benchResult {
	return benchResult{ID: item.ID, Doubled: item.Score * 2}
}

func BenchmarkLoopFilterMap(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		out := make([]benchResult, 0, len(source))
		for _, item := range source {
			if benchActive(item) {
				out = append(out, benchMap(item))
			}
		}
		benchSinkResult = out
	}
}

func BenchmarkLoopFilterMapControl(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		out := make([]benchResult, 0, len(source))
		for _, item := range source {
			if benchActive(item) {
				out = append(out, benchMap(item))
			}
		}
		benchSinkResult = out
	}
}

func BenchmarkSeqFilterMapFused(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkResult = functional.Slice(source).FilterMap(benchActive, benchMap)
	}
}

func BenchmarkSeqFilterMapChained(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkResult = functional.Slice(source).Filter(benchActive).Map(benchMap)
	}
}

func BenchmarkLoopMap(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		out := make([]benchResult, len(source))
		for i, item := range source {
			out[i] = benchMap(item)
		}
		benchSinkResult = out
	}
}

func BenchmarkSeqMap(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkResult = functional.Slice(source).Map(benchMap)
	}
}

// BenchmarkSeqFindFirstOnSource short-circuits over the source. The chained
// form below materializes the whole slice first, which is what makes it slow.
func BenchmarkSeqFindFirstOnSource(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkInt = functional.Slice(source).
			FindFirst(func(item benchItem) bool { return item.Active && item.Score == 7 }).
			Map(benchMap).
			Fold(func() int { return -1 }, func(r benchResult) int { return r.Doubled })
	}
}

func BenchmarkSeqFindFirstAfterChain(b *testing.B) {
	source := benchItems()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkInt = functional.Slice(source).
			Filter(benchActive).
			Map(benchMap).
			FindFirst(func(r benchResult) bool { return r.Doubled == 14 }).
			Fold(func() int { return -1 }, func(r benchResult) int { return r.Doubled })
	}
}
