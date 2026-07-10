package zopfli

import (
	"bytes"
	"math"
	"slices"
	"testing"
)

func cacheIntegrationStats(in []byte, instart, inend int) symbolStats {
	var state blockState
	state.initWithCache(nil, instart, inend, false)
	var store lz77Store
	store.init(in)
	var h hash
	h.alloc()
	lz77Greedy(&state, in, instart, inend, &store, &h)
	var stats symbolStats
	getStatistics(&store, &stats)
	return stats
}

func assertCacheIntegrationStoreEqual(t *testing.T, got, want *lz77Store) {
	t.Helper()
	if got.size != want.size ||
		!slices.Equal(got.litlens, want.litlens) ||
		!slices.Equal(got.dists, want.dists) ||
		!slices.Equal(got.pos, want.pos) ||
		!slices.Equal(got.llSymbol, want.llSymbol) ||
		!slices.Equal(got.dSymbol, want.dSymbol) ||
		!slices.Equal(got.llCounts, want.llCounts) ||
		!slices.Equal(got.dCounts, want.dCounts) {
		t.Fatalf("stores differ:\n got size=%d lengths=%v distances=%v positions=%v\nwant size=%d lengths=%v distances=%v positions=%v",
			got.size, got.litlens, got.dists, got.pos,
			want.size, want.litlens, want.dists, want.pos)
	}
}

func assertCacheIntegrationFloatSlicesEqual(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("cost lengths differ: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
			t.Fatalf("cost[%d] differs: got %.17g (%016x), want %.17g (%016x)",
				i, got[i], math.Float64bits(got[i]), want[i], math.Float64bits(want[i]))
		}
	}
}

func assertFullyCachedBlockMatchesUncached(t *testing.T, in []byte, instart, inend int) {
	t.Helper()
	blockSize := inend - instart

	var cachedState blockState
	cachedState.initWithCache(nil, instart, inend, true)
	buildLongestMatchCache(&cachedState, in, instart, inend, nil)
	if !cachedState.lmc.fullyBuilt() {
		t.Fatal("cache was not fully built")
	}

	var plainState blockState
	plainState.initWithCache(nil, instart, inend, false)

	var cachedGreedy, plainGreedy lz77Store
	cachedGreedy.init(in)
	plainGreedy.init(in)
	lz77Greedy(&cachedState, in, instart, inend, &cachedGreedy, nil)
	var greedyHash hash
	greedyHash.alloc()
	lz77Greedy(&plainState, in, instart, inend, &plainGreedy, &greedyHash)
	assertCacheIntegrationStoreEqual(t, &cachedGreedy, &plainGreedy)

	stats := cacheIntegrationStats(in, instart, inend)
	cachedStatLengths := make([]uint16, blockSize+1)
	plainStatLengths := make([]uint16, blockSize+1)
	cachedStatCosts := make([]float64, blockSize+1)
	plainStatCosts := make([]float64, blockSize+1)
	cachedStatCost := getBestLengthsStat(&cachedState, in, instart, inend, &stats, cachedStatLengths, nil, cachedStatCosts)
	var statHash hash
	statHash.alloc()
	plainStatCost := getBestLengthsStat(&plainState, in, instart, inend, &stats, plainStatLengths, &statHash, plainStatCosts)
	if math.Float64bits(cachedStatCost) != math.Float64bits(plainStatCost) {
		t.Fatalf("statistical final costs differ: got %.17g, want %.17g", cachedStatCost, plainStatCost)
	}
	assertCacheIntegrationFloatSlicesEqual(t, cachedStatCosts, plainStatCosts)
	if !slices.Equal(cachedStatLengths, plainStatLengths) {
		t.Fatalf("statistical predecessor lengths differ:\n got %v\nwant %v", cachedStatLengths, plainStatLengths)
	}
	var cachedStatPath, plainStatPath []uint16
	traceBackwards(blockSize, cachedStatLengths, &cachedStatPath)
	traceBackwards(blockSize, plainStatLengths, &plainStatPath)
	if !slices.Equal(cachedStatPath, plainStatPath) {
		t.Fatalf("statistical paths differ: got %v, want %v", cachedStatPath, plainStatPath)
	}
	var cachedStatStore, plainStatStore lz77Store
	cachedStatStore.init(in)
	plainStatStore.init(in)
	followPath(&cachedState, in, instart, inend, cachedStatPath, &cachedStatStore, nil)
	followPath(&plainState, in, instart, inend, plainStatPath, &plainStatStore, &statHash)
	assertCacheIntegrationStoreEqual(t, &cachedStatStore, &plainStatStore)

	cachedFixedLengths := make([]uint16, blockSize+1)
	plainFixedLengths := make([]uint16, blockSize+1)
	cachedFixedCosts := make([]float64, blockSize+1)
	plainFixedCosts := make([]float64, blockSize+1)
	cachedFixedCost := getBestLengthsFixed(&cachedState, in, instart, inend, cachedFixedLengths, nil, cachedFixedCosts)
	var fixedHash hash
	fixedHash.alloc()
	plainFixedCost := getBestLengthsFixed(&plainState, in, instart, inend, plainFixedLengths, &fixedHash, plainFixedCosts)
	if math.Float64bits(cachedFixedCost) != math.Float64bits(plainFixedCost) {
		t.Fatalf("fixed final costs differ: got %.17g, want %.17g", cachedFixedCost, plainFixedCost)
	}
	assertCacheIntegrationFloatSlicesEqual(t, cachedFixedCosts, plainFixedCosts)
	if !slices.Equal(cachedFixedLengths, plainFixedLengths) {
		t.Fatalf("fixed predecessor lengths differ:\n got %v\nwant %v", cachedFixedLengths, plainFixedLengths)
	}
	var cachedFixedPath, plainFixedPath []uint16
	traceBackwards(blockSize, cachedFixedLengths, &cachedFixedPath)
	traceBackwards(blockSize, plainFixedLengths, &plainFixedPath)
	if !slices.Equal(cachedFixedPath, plainFixedPath) {
		t.Fatalf("fixed paths differ: got %v, want %v", cachedFixedPath, plainFixedPath)
	}
	var cachedFixedStore, plainFixedStore lz77Store
	cachedFixedStore.init(in)
	plainFixedStore.init(in)
	followPath(&cachedState, in, instart, inend, cachedFixedPath, &cachedFixedStore, nil)
	followPath(&plainState, in, instart, inend, plainFixedPath, &plainFixedStore, &fixedHash)
	assertCacheIntegrationStoreEqual(t, &cachedFixedStore, &plainFixedStore)
}

func cacheIntegrationMixedFixture() []byte {
	data := make([]byte, 6*1024)
	state := uint32(0x12345678)
	for i := range data {
		state = state*1664525 + 1013904223
		data[i] = byte(state >> 24)
	}
	copy(data[1536:2560], data[127:1151])
	copy(data[3072:4096], data[1536:2560])
	pattern := []byte("cache integration: alpha beta gamma delta; ")
	for i := 4608; i < len(data); i++ {
		data[i] = pattern[(i-4608)%len(pattern)]
	}
	return data
}

func TestCompleteCacheMatchesUncachedOptimalParses(t *testing.T) {
	fixtures := []struct {
		name string
		data []byte
	}{
		{name: "repetitive", data: bytes.Repeat([]byte("mississippi banana bandana|0123456789|"), 96)},
		{name: "mixed", data: cacheIntegrationMixedFixture()},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			assertFullyCachedBlockMatchesUncached(t, fixture.data, 0, len(fixture.data))
		})
	}
}

func exerciseCompleteCacheWithNilHash(t *testing.T, in []byte, instart, inend int) {
	t.Helper()
	blockSize := inend - instart
	var state blockState
	state.initWithCache(nil, instart, inend, true)
	buildLongestMatchCache(&state, in, instart, inend, nil)
	if !state.lmc.fullyBuilt() {
		t.Fatal("cache was not fully built")
	}
	var greedy lz77Store
	greedy.init(in)
	lz77Greedy(&state, in, instart, inend, &greedy, nil)
	stats := cacheIntegrationStats(in, instart, inend)
	statLengths := make([]uint16, blockSize+1)
	statCosts := make([]float64, blockSize+1)
	getBestLengthsStat(&state, in, instart, inend, &stats, statLengths, nil, statCosts)
	var statPath []uint16
	traceBackwards(blockSize, statLengths, &statPath)
	var statStore lz77Store
	statStore.init(in)
	followPath(&state, in, instart, inend, statPath, &statStore, nil)
	fixedLengths := make([]uint16, blockSize+1)
	fixedCosts := make([]float64, blockSize+1)
	getBestLengthsFixed(&state, in, instart, inend, fixedLengths, nil, fixedCosts)
	var fixedPath []uint16
	traceBackwards(blockSize, fixedLengths, &fixedPath)
	var fixedStore lz77Store
	fixedStore.init(in)
	followPath(&state, in, instart, inend, fixedPath, &fixedStore, nil)
}

func TestCompleteCacheNilHashEdgeBlocks(t *testing.T) {
	t.Run("nil-empty", func(t *testing.T) {
		exerciseCompleteCacheWithNilHash(t, nil, 0, 0)
	})
	for _, test := range []struct {
		name         string
		data         []byte
		instart, end int
	}{
		{name: "one-byte", data: []byte("x"), end: 1},
		{name: "two-bytes", data: []byte("xy"), end: 2},
		{name: "empty-nonzero-offset", data: []byte("prefix-data"), instart: 6, end: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFullyCachedBlockMatchesUncached(t, test.data, test.instart, test.end)
		})
	}
}

func TestCompleteCacheBlockStartBeyondWindow(t *testing.T) {
	instart := windowSize + 257
	in := make([]byte, instart+1536)
	state := uint32(7)
	for i := range instart {
		state = state*1103515245 + 12345
		in[i] = byte(state >> 24)
	}
	copy(in[instart:instart+512], in[instart-4096:instart-3584])
	copy(in[instart+512:instart+1024], in[instart:instart+512])
	pattern := []byte("beyond-one-window-cache-history|")
	for i := instart + 1024; i < len(in); i++ {
		in[i] = pattern[(i-instart-1024)%len(pattern)]
	}
	assertFullyCachedBlockMatchesUncached(t, in, instart, len(in))
}

func naturalOverflowFixture() ([]byte, int) {
	target := make([]byte, 64)
	for i := range target {
		target[i] = toUint8((0x31 + i*73) & 0xff)
	}
	var in []byte
	for length := 20; length >= minMatch; length-- {
		in = append(in, target[:length]...)
		in = append(in, target[length]^0xff, 0, 0, 0, 0)
	}
	instart := len(in)
	in = append(in, target...)
	return in, instart
}

func TestCompleteCacheNaturalOverflowMatchesUncached(t *testing.T) {
	in, instart := naturalOverflowFixture()
	var state blockState
	state.initWithCache(nil, instart, len(in), true)
	buildLongestMatchCache(&state, in, instart, len(in), nil)
	if got := int(state.lmc.runCount[0]); got <= cacheLength {
		t.Fatalf("constructed match has %d distance runs, want more than inline capacity %d", got, cacheLength)
	}
	if len(state.lmc.runs) == 0 {
		t.Fatal("constructed match did not use flat run storage")
	}
	assertFullyCachedBlockMatchesUncached(t, in, instart, len(in))
}

func TestBudgetLimitedCacheMatchesUncachedOptimalParses(t *testing.T) {
	in, _ := pathologicalOverflowCandidates()
	var cache longestMatchCache
	partial := blockState{lmc: &cache}
	var partialHash hash
	buildLongestMatchCacheWithRunBudget(&partial, in, 0, len(in), &partialHash, 0)
	if cache.fullyBuilt() || !cache.runBudgetExceeded {
		t.Fatalf("budget-limited cache state = (built=%v exceeded=%v), want (false, true)", cache.fullyBuilt(), cache.runBudgetExceeded)
	}

	var plain blockState
	plain.initWithCache(nil, 0, len(in), false)
	stats := cacheIntegrationStats(in, 0, len(in))
	blockSize := len(in)

	partialStatLengths := make([]uint16, blockSize+1)
	plainStatLengths := make([]uint16, blockSize+1)
	partialStatCosts := make([]float64, blockSize+1)
	plainStatCosts := make([]float64, blockSize+1)
	var plainHash hash
	plainHash.alloc()
	partialHash.alloc()
	partialStatCost := getBestLengthsStat(&partial, in, 0, len(in), &stats, partialStatLengths, &partialHash, partialStatCosts)
	plainStatCost := getBestLengthsStat(&plain, in, 0, len(in), &stats, plainStatLengths, &plainHash, plainStatCosts)
	if math.Float64bits(partialStatCost) != math.Float64bits(plainStatCost) {
		t.Fatalf("budget fallback statistical cost = %.17g, want %.17g", partialStatCost, plainStatCost)
	}
	assertCacheIntegrationFloatSlicesEqual(t, partialStatCosts, plainStatCosts)
	if !slices.Equal(partialStatLengths, plainStatLengths) {
		t.Fatal("budget fallback statistical predecessors differ")
	}
	var partialStatPath, plainStatPath []uint16
	traceBackwards(blockSize, partialStatLengths, &partialStatPath)
	traceBackwards(blockSize, plainStatLengths, &plainStatPath)
	if !slices.Equal(partialStatPath, plainStatPath) {
		t.Fatal("budget fallback statistical paths differ")
	}
	var partialStatStore, plainStatStore lz77Store
	partialStatStore.init(in)
	plainStatStore.init(in)
	followPath(&partial, in, 0, len(in), partialStatPath, &partialStatStore, &partialHash)
	followPath(&plain, in, 0, len(in), plainStatPath, &plainStatStore, &plainHash)
	assertCacheIntegrationStoreEqual(t, &partialStatStore, &plainStatStore)

	partialFixedLengths := make([]uint16, blockSize+1)
	plainFixedLengths := make([]uint16, blockSize+1)
	partialFixedCosts := make([]float64, blockSize+1)
	plainFixedCosts := make([]float64, blockSize+1)
	partialFixedCost := getBestLengthsFixed(&partial, in, 0, len(in), partialFixedLengths, &partialHash, partialFixedCosts)
	plainFixedCost := getBestLengthsFixed(&plain, in, 0, len(in), plainFixedLengths, &plainHash, plainFixedCosts)
	if math.Float64bits(partialFixedCost) != math.Float64bits(plainFixedCost) {
		t.Fatalf("budget fallback fixed cost = %.17g, want %.17g", partialFixedCost, plainFixedCost)
	}
	assertCacheIntegrationFloatSlicesEqual(t, partialFixedCosts, plainFixedCosts)
	if !slices.Equal(partialFixedLengths, plainFixedLengths) {
		t.Fatal("budget fallback fixed predecessors differ")
	}
	var partialFixedPath, plainFixedPath []uint16
	traceBackwards(blockSize, partialFixedLengths, &partialFixedPath)
	traceBackwards(blockSize, plainFixedLengths, &plainFixedPath)
	if !slices.Equal(partialFixedPath, plainFixedPath) {
		t.Fatal("budget fallback fixed paths differ")
	}
	var partialFixedStore, plainFixedStore lz77Store
	partialFixedStore.init(in)
	plainFixedStore.init(in)
	followPath(&partial, in, 0, len(in), partialFixedPath, &partialFixedStore, &partialHash)
	followPath(&plain, in, 0, len(in), plainFixedPath, &plainFixedStore, &plainHash)
	assertCacheIntegrationStoreEqual(t, &partialFixedStore, &plainFixedStore)
}
