package functional_test

import (
	"strconv"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/pkg/functional"
)

const benchSize = 1000

var (
	benchSinkStrings []string
	benchSinkInt     int
)

func benchInts() []int {
	out := make([]int, benchSize)
	for i := range out {
		out[i] = i
	}
	return out
}

func BenchmarkSeqMap(b *testing.B) {
	source := benchInts()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkStrings = functional.Slice(source).Map(strconv.Itoa)
	}
}

func BenchmarkLoopMap(b *testing.B) {
	source := benchInts()
	b.ReportAllocs()
	for b.Loop() {
		out := make([]string, len(source))
		for i, item := range source {
			out[i] = strconv.Itoa(item)
		}
		benchSinkStrings = out
	}
}

func BenchmarkSeqFilterMap(b *testing.B) {
	source := benchInts()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkStrings = functional.Slice(source).
			Filter(func(i int) bool { return i%2 == 0 }).
			Map(strconv.Itoa)
	}
}

func BenchmarkLoopFilterMap(b *testing.B) {
	source := benchInts()
	b.ReportAllocs()
	for b.Loop() {
		out := make([]string, 0, len(source))
		for _, item := range source {
			if item%2 == 0 {
				out = append(out, strconv.Itoa(item))
			}
		}
		benchSinkStrings = out
	}
}

func BenchmarkSeqFindFirst(b *testing.B) {
	source := benchInts()
	b.ReportAllocs()
	for b.Loop() {
		benchSinkInt = functional.Slice(source).FindFirst(func(i int) bool { return i == benchSize-1 }).OrElse(0)
	}
}
