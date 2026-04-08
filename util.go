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

//nolint:gosec // Range is validated before narrowing.
func toUint16(value int) uint16 {
	must(value >= 0 && value <= math.MaxUint16, "uint16 overflow")
	return uint16(value)
}

//nolint:gosec // Range is validated before narrowing.
func toUint16FromInt32(value int32) uint16 {
	must(value >= 0 && value <= math.MaxUint16, "uint16 overflow")
	return uint16(value)
}

//nolint:gosec // Range is validated before narrowing.
func toUint32(value int) uint32 {
	must(value >= 0 && uint64(value) <= math.MaxUint32, "uint32 overflow")
	return uint32(value)
}

//nolint:gosec // Range is validated before narrowing.
func toInt32(value int) int32 {
	must(value >= math.MinInt32 && value <= math.MaxInt32, "int32 overflow")
	return int32(value)
}

//nolint:gosec // Range is validated before narrowing.
func toUint8(value int) uint8 {
	must(value >= 0 && value <= math.MaxUint8, "uint8 overflow")
	return uint8(value)
}

func lowByteFromUint32(value uint32) byte {
	return byte(value & 0xff)
}

func lowByteFromInt(value int) byte {
	return byte(value & 0xff)
}
