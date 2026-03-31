package zopfli

import (
	"fmt"
	"math"
	"os"
)

const (
	maxMatch         = 258
	minMatch         = 3
	numLL            = 288
	numD             = 32
	windowSize       = 32768
	windowMask       = windowSize - 1
	masterBlockSize  = 1000000
	largeFloat       = 1e30
	cacheLength      = 8
	maxChainHits     = 8192
	enableAssertions = false
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func must(cond bool, msg string) {
	if enableAssertions && !cond {
		panic(msg)
	}
}

func debugf(opt *Options, format string, args ...any) {
	if opt != nil && opt.Verbose {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func clampNearZero(v float64) float64 {
	if v < 0 && v > -1e-5 {
		return 0
	}
	return v
}

func log2(v float64) float64 {
	return math.Log2(v)
}
