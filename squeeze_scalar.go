//go:build !goexperiment.simd || !amd64

package zopfli

func firstImprovableLength(costs []float64, start, end int, threshold float64) int {
	return firstImprovableLengthScalar(costs, start, end, threshold)
}
