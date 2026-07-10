package zopfli

import (
	"slices"
	"testing"
)

var benchmarkLZ77StoreSize int

func TestSublenPublicationStartsAtMinimumMatch(t *testing.T) {
	var lmc longestMatchCache
	lmc.init(1)
	var sublen [maxMatch + 1]uint16
	sublen[minMatch-1] = 17
	sublen[minMatch] = 23
	if !lmc.storeMatch(0, 23, minMatch, 0, &sublen, true) {
		t.Fatal("minimum-length match was not cached")
	}
	runs, ok := lmc.cachedSublenRuns(0)
	if !ok || runs.count() != 1 {
		t.Fatalf("usable run view = (count=%d ok=%v), want (1, true)", runs.count(), ok)
	}
	end, distance := runs.at(0)
	if end != minMatch || distance != 23 {
		t.Fatalf("first usable run = (%d, %d), want (%d, 23)", end, distance, minMatch)
	}
}

func TestLongestMatchCacheReuseDoesNotRequireClearingRunStorage(t *testing.T) {
	var lmc longestMatchCache
	lmc.init(2)
	fillUint16s(lmc.dist, 0xffff)
	lmc.runs = append(lmc.runs, sublenRun{end: 0xffff, dist: 0xffff})
	lmc.init(2)

	state := blockState{lmc: &lmc, blockstart: 0, blockend: 2}
	for pos := range 2 {
		limit := maxMatch
		var sublen [maxMatch + 1]uint16
		if _, _, ok := tryGetFromLongestMatchCache(&state, pos, &limit, &sublen); ok {
			t.Fatalf("dirty, uninitialized cache position %d was reported as cached", pos)
		}
	}

	var sublen [maxMatch + 1]uint16
	sublen[minMatch] = 7
	fillUint16s(sublen[minMatch+1:7], 11)
	if !lmc.storeMatch(0, 11, 6, 0, &sublen, true) {
		t.Fatal("complete inline runs were not cached")
	}

	var decoded [maxMatch + 1]uint16
	fillUint16s(decoded[:], 0xffff)
	lmc.cacheToSublen(0, 6, &decoded)
	want := [...]uint16{0xffff, 0xffff, 0xffff, 7, 11, 11, 11}
	if !slices.Equal(decoded[:len(want)], want[:]) {
		t.Fatalf("decoded sublengths = %v, want %v", decoded[:len(want)], want)
	}
	_, ends, dists, compact := lmc.compactSublen(0, 6)
	if !compact {
		t.Fatal("inline-sized flat run entry was not available through the legacy compact accessor")
	}
	for i := 2; i < cacheLength; i++ {
		if ends[i] != 6 || dists[i] != 0 {
			t.Fatalf("padding run %d = (%d, %d), want (6, 0)", i, ends[i], dists[i])
		}
	}
}

func TestGreedyParseWarmsLongestMatchCacheWithoutChangingParse(t *testing.T) {
	data := []byte("mississippi banana bandana|0123456789|")
	data = slices.Repeat(data, 32)

	var cachedState blockState
	cachedState.initWithCache(nil, 0, len(data), true)
	var cachedStore lz77Store
	cachedStore.init(data)
	var cachedHash hash
	cachedHash.alloc()
	lz77Greedy(&cachedState, data, 0, len(data), &cachedStore, &cachedHash)

	var plainState blockState
	plainState.initWithCache(nil, 0, len(data), false)
	var plainStore lz77Store
	plainStore.init(data)
	var plainHash hash
	plainHash.alloc()
	lz77Greedy(&plainState, data, 0, len(data), &plainStore, &plainHash)

	if cachedStore.size != plainStore.size ||
		!slices.Equal(cachedStore.litlens, plainStore.litlens) ||
		!slices.Equal(cachedStore.dists, plainStore.dists) ||
		!slices.Equal(cachedStore.pos, plainStore.pos) {
		t.Fatalf("cached greedy parse differs from uncached parse:\n cached lengths=%v dists=%v pos=%v\n plain lengths=%v dists=%v pos=%v",
			cachedStore.litlens, cachedStore.dists, cachedStore.pos,
			plainStore.litlens, plainStore.dists, plainStore.pos)
	}

	computed := 0
	matches := 0
	for i := range cachedState.lmc.length {
		if cachedState.lmc.entryFilled(i) {
			computed++
			if cachedState.lmc.dist[i] != 0 {
				matches++
			}
		}
	}
	if computed == 0 || matches == 0 {
		t.Fatalf("greedy parse warmed %d positions and %d matches, want both non-zero", computed, matches)
	}
}

func TestLongestMatchCacheStoresCompleteOverflowRuns(t *testing.T) {
	var lmc longestMatchCache
	lmc.init(3)
	if lmc.fullyBuilt() {
		t.Fatal("fresh cache was reported as fully built")
	}
	if _, _, _, ok := lmc.cachedLongestMatch(0); ok {
		t.Fatal("unfilled position was reported as cached")
	}

	lastLength := minMatch + cacheLength + 4
	var sublen [maxMatch + 1]uint16
	for length := minMatch; length <= lastLength; length++ {
		sublen[length] = toUint16(1000 + length)
	}
	if !lmc.storeMatch(0, toUint16(1000+lastLength), toUint16(lastLength), 37, &sublen, true) {
		t.Fatal("complete overflow runs were not cached")
	}
	runCount := lastLength - minMatch + 1

	distance, length, same, ok := lmc.cachedLongestMatch(0)
	if !ok || int(length) != lastLength || int(distance) != 1000+lastLength || same != 37 {
		t.Fatalf("longest match = (distance=%d length=%d same=%d ok=%v)", distance, length, same, ok)
	}
	state := blockState{lmc: &lmc, blockstart: 0, blockend: 3}
	limit := minMatch
	distance, length, ok = tryGetFromLongestMatchCache(&state, 0, &limit, nil)
	if ok {
		t.Fatalf("nil-sublen truncated match unexpectedly used the full-match distance: distance=%d length=%d", distance, length)
	}
	view, ok := lmc.cachedSublenRuns(0)
	if !ok || view.count() != runCount || len(view.runs) != runCount {
		t.Fatalf("run view = (count=%d stored=%d ok=%v), want (%d, %d, true)",
			view.count(), len(view.runs), ok, runCount, runCount)
	}
	for i := 0; i < view.count(); i++ {
		end, runDistance := view.at(i)
		wantEnd := minMatch + i
		if int(end) != wantEnd || int(runDistance) != 1000+wantEnd {
			t.Fatalf("run %d = (%d, %d), want (%d, %d)", i, end, runDistance, wantEnd, 1000+wantEnd)
		}
		cachedDistance, found := lmc.cachedDistanceForLength(0, wantEnd)
		if !found || cachedDistance != runDistance {
			t.Fatalf("distance for length %d = (%d, %v), want (%d, true)", wantEnd, cachedDistance, found, runDistance)
		}
	}
	if _, _, _, compact := lmc.compactSublen(0, lastLength); compact {
		t.Fatal("legacy inline-only accessor accepted an overflow entry")
	}

	lmc.storeMatch(1, 0, 0, 9, nil, true)
	distance, length, same, ok = lmc.cachedLongestMatch(1)
	if !ok || distance != 0 || length != 0 || same != 9 {
		t.Fatalf("cached no-match = (distance=%d length=%d same=%d ok=%v)", distance, length, same, ok)
	}
	noMatchRuns, ok := lmc.cachedSublenRuns(1)
	if !ok || noMatchRuns.count() != 0 {
		t.Fatalf("no-match run view = (count=%d ok=%v), want (0, true)", noMatchRuns.count(), ok)
	}
	lmc.storeMatch(2, 7, minMatch+1, 4, &sublen, false)
	if !lmc.entryFilled(2) || lmc.entryComplete(2) {
		t.Fatalf("incomplete position state = (filled=%v complete=%v), want (true, false)", lmc.entryFilled(2), lmc.entryComplete(2))
	}
	if _, _, _, ok := lmc.cachedLongestMatch(2); ok {
		t.Fatal("incomplete position was returned through the complete accessor")
	}

	runCapacity := cap(lmc.runs)
	lmc.init(3)
	if len(lmc.runs) != 0 || cap(lmc.runs) != runCapacity {
		t.Fatalf("run arena after reuse = len %d cap %d, want len 0 cap %d", len(lmc.runs), cap(lmc.runs), runCapacity)
	}
	if _, _, _, ok := lmc.cachedLongestMatch(0); ok {
		t.Fatal("reinitialized position retained its filled state")
	}
}

func TestLongestMatchCacheRunBudgetIsAtomicAndBounded(t *testing.T) {
	const runBudget = 4
	var lmc longestMatchCache
	lmc.initWithRunBudget(1, runBudget)
	lastLength := minMatch + cacheLength + runBudget
	var sublen [maxMatch + 1]uint16
	for length := minMatch; length <= lastLength; length++ {
		sublen[length] = toUint16(2000 + length)
	}

	if lmc.storeMatch(0, sublen[lastLength], toUint16(lastLength), 0, &sublen, true) {
		t.Fatal("over-budget candidate set was reported as complete")
	}
	if !lmc.runBudgetExceeded || !lmc.entryFilled(0) || lmc.entryComplete(0) {
		t.Fatalf("over-budget state = (exceeded=%v filled=%v complete=%v), want (true, true, false)",
			lmc.runBudgetExceeded, lmc.entryFilled(0), lmc.entryComplete(0))
	}
	if len(lmc.runs) != 0 || cap(lmc.runs) > runBudget {
		t.Fatalf("run arena = len %d cap %d, want len 0 and cap <= %d", len(lmc.runs), cap(lmc.runs), runBudget)
	}
}

func pathologicalOverflowCandidates() ([]byte, int) {
	target := make([]byte, 64)
	for i := range target {
		target[i] = byte(i + 1)
	}
	data := make([]byte, 0, 512)
	for matchLength := 20; matchLength >= minMatch; matchLength-- {
		data = append(data, target[:matchLength]...)
		data = append(data, 0)
	}
	targetPos := len(data)
	data = append(data, target...)
	return data, targetPos
}

func TestBuildLongestMatchCacheStopsAtOverflowBudgetAndGreedyFallsBack(t *testing.T) {
	data, targetPos := pathologicalOverflowCandidates()
	var lmc longestMatchCache
	state := blockState{lmc: &lmc}
	var h hash
	buildLongestMatchCacheWithRunBudget(&state, data, 0, len(data), &h, 0)

	if lmc.fullyBuilt() || !lmc.runBudgetExceeded {
		t.Fatalf("budget-limited build = (built=%v exceeded=%v), want (false, true)", lmc.fullyBuilt(), lmc.runBudgetExceeded)
	}
	failurePos := -1
	for pos := 0; pos <= targetPos; pos++ {
		if lmc.entryFilled(pos) && !lmc.entryComplete(pos) {
			failurePos = pos
			break
		}
	}
	if failurePos == -1 {
		t.Fatal("budget-limited discovery did not publish an incomplete entry")
	}
	if failurePos+1 >= len(data) || lmc.entryFilled(failurePos+1) {
		t.Fatal("discovery did not stop immediately after exhausting the overflow budget")
	}

	var cachedStore lz77Store
	cachedStore.init(data)
	lz77Greedy(&state, data, 0, len(data), &cachedStore, &h)

	var plainState blockState
	plainState.initWithCache(nil, 0, len(data), false)
	var plainStore lz77Store
	plainStore.init(data)
	var plainHash hash
	plainHash.alloc()
	lz77Greedy(&plainState, data, 0, len(data), &plainStore, &plainHash)

	if cachedStore.size != plainStore.size ||
		!slices.Equal(cachedStore.litlens, plainStore.litlens) ||
		!slices.Equal(cachedStore.dists, plainStore.dists) ||
		!slices.Equal(cachedStore.pos, plainStore.pos) {
		t.Fatalf("budget fallback parse differs from uncached parse:\n cached lengths=%v dists=%v pos=%v\n plain lengths=%v dists=%v pos=%v",
			cachedStore.litlens, cachedStore.dists, cachedStore.pos,
			plainStore.litlens, plainStore.dists, plainStore.pos)
	}
}

func TestBuildLongestMatchCacheFillsEveryPosition(t *testing.T) {
	prefix := []byte("prefix-window-data|prefix-window-data|")
	payload := slices.Repeat([]byte("mississippi banana bandana|0123456789|"), 40)
	data := append(slices.Clone(prefix), payload...)
	instart := len(prefix) - 11
	inend := len(data)

	var lmc longestMatchCache
	state := blockState{lmc: &lmc}
	var cacheHash hash
	buildLongestMatchCache(&state, data, instart, inend, &cacheHash)
	if !lmc.fullyBuilt() {
		t.Fatal("one-pass cache was not marked fully built")
	}

	if len(lmc.length) != inend-instart || len(lmc.same) != inend-instart {
		t.Fatalf("cache metadata lengths = (%d, %d), want %d", len(lmc.length), len(lmc.same), inend-instart)
	}
	for pos := range lmc.length {
		if !lmc.entryFilled(pos) || !lmc.entryComplete(pos) {
			t.Fatalf("cache position %d state = (filled=%v complete=%v), want both true", pos, lmc.entryFilled(pos), lmc.entryComplete(pos))
		}
	}

	var plainState blockState
	plainState.initWithCache(nil, instart, inend, false)
	var plainHash hash
	plainHash.alloc()
	plainHash.reset()
	windowStart := maxInt(0, instart-windowSize)
	plainHash.warmup(data, windowStart, inend)
	for i := windowStart; i < instart; i++ {
		plainHash.update(data, i, inend)
	}
	var sublen [maxMatch + 1]uint16
	for pos := instart; pos < inend; pos++ {
		plainHash.update(data, pos, inend)
		wantDistance, wantLength := findLongestMatch(&plainState, &plainHash, data, pos, inend, maxMatch, &sublen)
		if wantLength < minMatch {
			wantDistance = 0
			wantLength = 0
		}
		cachePos := pos - instart
		gotDistance, gotLength, gotSame, ok := lmc.cachedLongestMatch(cachePos)
		if !ok || gotDistance != wantDistance || gotLength != wantLength {
			t.Fatalf("position %d longest match = (%d, %d, %v), want (%d, %d, true)", pos, gotDistance, gotLength, ok, wantDistance, wantLength)
		}
		wantSame := 0
		for pos+wantSame+1 < inend && data[pos] == data[pos+wantSame+1] && wantSame < 0xffff {
			wantSame++
		}
		if int(gotSame) != wantSame {
			t.Fatalf("position %d same run = %d, want %d", pos, gotSame, wantSame)
		}
		for length := minMatch; length <= int(wantLength); length++ {
			got, found := lmc.cachedDistanceForLength(cachePos, length)
			if !found || got != sublen[length] {
				t.Fatalf("position %d distance for length %d = (%d, %v), want (%d, true)", pos, length, got, found, sublen[length])
			}
		}
	}
	for pos := inend - minMatch + 1; pos < inend; pos++ {
		_, length, _, ok := lmc.cachedLongestMatch(pos - instart)
		if !ok || length != 0 {
			t.Fatalf("terminal position %d = (length=%d ok=%v), want complete no-match", pos, length, ok)
		}
	}
}

func TestHashUsesMaskedHashDomain(t *testing.T) {
	var h hash
	h.alloc()
	if len(h.head) != hashSize || len(h.head2) != hashSize {
		t.Fatalf("hash heads have lengths (%d, %d), want (%d, %d)", len(h.head), len(h.head2), hashSize, hashSize)
	}

	data := make([]byte, 4*1024)
	var value uint32 = 1
	for i := range data {
		value = value*1664525 + 1013904223
		data[i] = byte(value >> 24)
	}
	h.reset()
	h.warmup(data, 0, len(data))
	for i := range data {
		h.update(data, i, len(data))
		if h.val < 0 || h.val >= hashSize || h.val2 < 0 || h.val2 >= hashSize {
			t.Fatalf("hash values at %d = (%d, %d), want [0, %d)", i, h.val, h.val2, hashSize)
		}
	}
}

func TestLiteralHeavyStoreReservationIsDensityGatedAndBounded(t *testing.T) {
	if got := literalHeavyStoreReserve(64*1024, lz77StoreDensitySampleBytes-1, lz77StoreDensitySampleBytes-1); got != 0 {
		t.Fatalf("reserve before sample = %d, want 0", got)
	}
	if got := literalHeavyStoreReserve(64*1024, 8*1024, 1024); got != 0 {
		t.Fatalf("reserve for sparse token stream = %d, want 0", got)
	}
	if got := literalHeavyStoreReserve(64*1024, 8*1024, 8*1024); got != 64*1024 {
		t.Fatalf("reserve for small dense block = %d, want %d", got, 64*1024)
	}
	if got := literalHeavyStoreReserve(16*lz77StoreMaxReservedTokens, 8*1024, 8*1024); got != lz77StoreMaxReservedTokens {
		t.Fatalf("reserve for large dense block = %d, want bounded %d", got, lz77StoreMaxReservedTokens)
	}
}

func TestReserveTokensPreservesStoreAndHistogram(t *testing.T) {
	data := []byte("abc")
	var store lz77Store
	store.init(data)
	store.storeLitLenDist('a', 0, 0)
	store.storeLitLenDist('b', 0, 1)
	store.reserveTokens(10_000)

	if store.size != 2 || !slices.Equal(store.litlens, []uint16{'a', 'b'}) || !slices.Equal(store.pos, []int{0, 1}) {
		t.Fatalf("store changed while reserving: size=%d lengths=%v pos=%v", store.size, store.litlens, store.pos)
	}
	if cap(store.litlens) < 10_000 || cap(store.dists) < 10_000 || cap(store.pos) < 10_000 ||
		cap(store.llSymbol) < 10_000 || cap(store.dSymbol) < 10_000 {
		t.Fatal("token arrays were not reserved together")
	}
	if cap(store.llCounts) < roundUpToMultiple(10_000, numLL) || cap(store.dCounts) < roundUpToMultiple(10_000, numD) {
		t.Fatal("histogram arrays were not reserved to their chunk boundaries")
	}
	if store.llCounts['a'] != 1 || store.llCounts['b'] != 1 {
		t.Fatalf("histogram changed while reserving: a=%d b=%d", store.llCounts['a'], store.llCounts['b'])
	}
}

func BenchmarkLZ77StoreLiteralHeavyGrowth(b *testing.B) {
	const tokenCount = 256 * 1024
	for _, test := range []struct {
		name    string
		reserve bool
	}{
		{name: "append"},
		{name: "density-reserved", reserve: true},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(tokenCount)
			for range b.N {
				var store lz77Store
				store.init(nil)
				if test.reserve {
					store.reserveTokens(lz77StoreDensitySampleBytes)
				}
				for i := range tokenCount {
					if test.reserve && i == lz77StoreDensitySampleBytes {
						store.reserveTokens(tokenCount)
					}
					store.storeLitLenDist(uint16(i&255), 0, i)
				}
				benchmarkLZ77StoreSize = store.size
			}
		})
	}
}
