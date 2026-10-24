//go:build goexperiment.simd && amd64

package zopfli

import (
	"math/bits"
	"simd/archsimd"
)

func firstImprovableLength(costs []float64, start, end int, threshold float64) int {
	if end-start < 3 || !archsimd.X86.AVX() {
		return firstImprovableLengthScalar(costs, start, end, threshold)
	}
	// Loading repeated scalars only needs AVX. Go 1.27's register broadcast
	// intrinsic requires AVX2, even for floating-point vectors.
	bound := archsimd.LoadFloat64x4Array(&[4]float64{threshold, threshold, threshold, threshold})
	for start+4 <= end+1 {
		current := archsimd.LoadFloat64x4(costs[start : start+4])
		if mask := current.Greater(bound).ToBits(); mask != 0 {
			archsimd.ClearAVXUpperBits()
			return start + bits.TrailingZeros8(mask)
		}
		start += 4
	}
	// Re-read the last complete vector for a partial tail. Its earlier lanes
	// already failed the comparison, so any hit still belongs to the tail.
	// Keeping the tail vectorized also avoids spilling a live scalar threshold
	// with an SSE instruction before VZEROUPPER on older CPUs.
	if start <= end {
		current := archsimd.LoadFloat64x4(costs[end-3 : end+1])
		if mask := current.Greater(bound).ToBits(); mask != 0 {
			archsimd.ClearAVXUpperBits()
			return end - 3 + bits.TrailingZeros8(mask)
		}
	}
	// Go 1.27 does not insert VZEROUPPER automatically. Clear before returning
	// to scalar Go, which uses SSE and otherwise pays an AVX transition penalty.
	archsimd.ClearAVXUpperBits()
	return end + 1
}
