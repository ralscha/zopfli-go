package zopfli

import (
	"crypto/sha256"
	"fmt"
	"math"
	"testing"
)

func TestSIMDCompressionMatchesMain(t *testing.T) {
	// Captured from main at 8dde94a7e261f0e05c42dfe334686cfee288086f.
	// The fixture is generated in Go, independent of checkout line endings.
	for _, options := range []Options{DefaultOptions(), FastOptions()} {
		for _, tc := range []struct {
			format Format
			digest string
		}{
			{FormatGzip, "7caa05e0f5be3d34435746a399b65be61257fce75771627c474b5b651f6d999d"},
			{FormatZlib, "d736b24cb63d37b4dd434ebfdd7c73e0aaf1a2d2bfb0ed607c0d0f063ef403d9"},
			{FormatDeflate, "5b0265c20e7eedc25c6bf6106a52714c4d0e339dc85fc767d721b2c1d30578d3"},
		} {
			out := Compress(&options, tc.format, deterministicOptimizationFixture())
			if got := fmt.Sprintf("%x", sha256.Sum256(out)); got != tc.digest {
				t.Fatalf("format %d, iterations %d: output differs from main: %s", tc.format, options.NumIterations, got)
			}
		}
	}
}

func TestFirstImprovableLengthBoundaries(t *testing.T) {
	// Exercise every match length, alignment and first matching lane, including
	// empty ranges and scalar tails. Values outside the range must be ignored.
	for start := 0; start < 8; start++ {
		for count := 0; count <= maxMatch+1-start; count++ {
			end := start + count - 1
			costs := make([]float64, end+2)
			costs[end+1] = 2
			for hit := start; hit <= end+1; hit++ {
				if hit <= end {
					costs[hit] = 2
				}
				if got := firstImprovableLength(costs[:end+1], start, end, 1); got != hit {
					t.Fatalf("range [%d,%d], hit %d: got %d", start, end, hit, got)
				}
				costs[hit] = 1 // Ties must not count as improvements.
			}
		}
	}
	for _, threshold := range []float64{0, math.Inf(1), math.Inf(-1), math.NaN()} {
		costs := []float64{math.NaN(), math.Inf(-1), -1, math.Copysign(0, -1), 0, 1, math.Inf(1)}
		for start := range costs {
			want := len(costs)
			for i := start; i < len(costs); i++ {
				if costs[i] > threshold {
					want = i
					break
				}
			}
			if got := firstImprovableLength(costs, start, len(costs)-1, threshold); got != want {
				t.Fatalf("threshold %v, start %d: got %d, want %d", threshold, start, got, want)
			}
		}
	}
}

var costScanBenchmarkResult int

func BenchmarkFirstImprovableLength(b *testing.B) {
	for _, count := range []int{3, 16, 64, 256} {
		for _, hit := range []int{0, count / 2, count} {
			b.Run(fmt.Sprintf("length-%d/hit-%d", count, hit), func(b *testing.B) {
				costs := make([]float64, count+minMatch)
				if hit < count {
					costs[minMatch+hit] = 2
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					costScanBenchmarkResult = firstImprovableLength(costs, minMatch, len(costs)-1, 1)
				}
			})
		}
	}
}
