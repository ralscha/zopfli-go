package zopfli

import (
	"fmt"
	"os"
)

type symbolStats struct {
	litlens   [numLL]int
	dists     [numD]int
	llSymbols [numLL]float64
	dSymbols  [numD]float64
}

type ranState struct {
	mw uint32
	mz uint32
}

func (s *symbolStats) init() {
	*s = symbolStats{}
}

func (s *symbolStats) copyFrom(src *symbolStats) {
	*s = *src
}

func addWeighedStatFreqs(stats1 *symbolStats, w1 float64, stats2 *symbolStats, w2 float64, result *symbolStats) {
	for i := range numLL {
		result.litlens[i] = int(float64(stats1.litlens[i])*w1 + float64(stats2.litlens[i])*w2)
	}
	for i := range numD {
		result.dists[i] = int(float64(stats1.dists[i])*w1 + float64(stats2.dists[i])*w2)
	}
	result.litlens[256] = 1
}

func (r *ranState) init() {
	r.mw = 1
	r.mz = 2
}

func (r *ranState) next() uint32 {
	r.mz = 36969*(r.mz&65535) + (r.mz >> 16)
	r.mw = 18000*(r.mw&65535) + (r.mw >> 16)
	return (r.mz << 16) + r.mw
}

func randomizeFreqs(state *ranState, freqs []int) {
	for i := range freqs {
		if (state.next()>>4)%3 == 0 {
			freqs[i] = freqs[int(state.next())%len(freqs)]
		}
	}
}

func randomizeStatFreqs(state *ranState, stats *symbolStats) {
	randomizeFreqs(state, stats.litlens[:])
	randomizeFreqs(state, stats.dists[:])
	stats.litlens[256] = 1
}

// firstImprovableLength returns the smallest length in [start, end] whose
// current cost is above threshold, or end+1 when the whole range is already at
// or below it.
//
// Callers pass a lower bound on the cost of every candidate they are about to
// consider, so a length whose recorded cost is already at or below the bound
// can never be improved. Skipping is optional, which is why the caller may then
// relax the whole remainder of the range without re-testing.
func firstImprovableLength(costs []float64, start, end int, threshold float64) int {
	for index, current := range costs[start : end+1] {
		if current > threshold {
			return start + index
		}
	}
	return end + 1
}

// relaxLengthSegment relaxes the dynamic programming costs for every length in
// [start, end] against a single candidate cost. Callers split match ranges on
// length-symbol boundaries so that the candidate cost is constant here, which
// makes the comparison below both the improvement test and an exact guard.
func relaxLengthSegment(costs []float64, lengths []uint16, start, end int, newCost float64) {
	count := end + 1 - start
	segment := costs[start : start+count]
	segmentLengths := lengths[start : start+count]
	for index, current := range segment {
		if newCost < current {
			segment[index] = newCost
			segmentLengths[index] = toUint16(start + index)
		}
	}
}

// relaxStatLengths relaxes [start, end] against the entropy cost model, where
// the candidate cost varies with the length symbol of each length.
func relaxStatLengths(costs []float64, lengths []uint16, start, end int, basePlusDist float64, lengthCosts *[maxMatch + 1]float64) {
	count := end + 1 - start
	segment := costs[start : start+count]
	segmentLengths := lengths[start : start+count]
	segmentCosts := lengthCosts[start : start+count]
	for index, current := range segment {
		newCost := basePlusDist + segmentCosts[index]
		if newCost < current {
			segment[index] = newCost
			segmentLengths[index] = toUint16(start + index)
		}
	}
}

func relaxFixedLengthRanges(costs []float64, lengths []uint16, start, end int, baseCost float64, distValue int) {
	distExtraPlusFive := getDistExtraBits(distValue) + 5
	for start <= end {
		rangeEnd := minInt(int(lengthSymbolRunEnd[start]), end)
		lengthSymbol := getLengthSymbol(start)
		newCost := baseCost + float64(getLengthExtraBits(start)+distExtraPlusFive)
		if lengthSymbol <= 279 {
			newCost += 7
		} else {
			newCost += 8
		}
		relaxLengthSegment(costs, lengths, start, rangeEnd, newCost)
		start = rangeEnd + 1
	}
}

func getBestLengthsStat(s *blockState, in []byte, instart, inend int, stats *symbolStats, lengthArray []uint16, h *hash, costs []float64) float64 {
	blocksize := inend - instart
	useCompleteCache := s.lmc != nil && s.lmc.fullyBuilt()
	llSymbols := stats.llSymbols[:]
	dSymbols := stats.dSymbols[:]
	var literalCosts [256]float64
	copy(literalCosts[:], llSymbols[:256])
	var lengthCosts [maxMatch + 1]float64
	for length := 3; length <= maxMatch; length++ {
		lengthCosts[length] = float64(getLengthExtraBits(length)) + llSymbols[getLengthSymbol(length)]
	}
	// lengthCostSuffixMin[n] is the cheapest length term of any match of at
	// least n bytes. Added to a match's distance term it lower-bounds every
	// candidate cost that a run starting at n can produce, which makes it a
	// much tighter relaxation guard than the global cost model minimum.
	var lengthCostSuffixMin [maxMatch + 2]float64
	lengthCostSuffixMin[maxMatch+1] = largeFloat
	for length := maxMatch; length >= minMatch; length-- {
		suffixMin := lengthCostSuffixMin[length+1]
		if lengthCosts[length] < suffixMin {
			suffixMin = lengthCosts[length]
		}
		lengthCostSuffixMin[length] = suffixMin
	}
	maxMatchCost := lengthCosts[maxMatch] + dSymbols[0]
	if !useCompleteCache {
		windowStart := 0
		if instart > windowSize {
			windowStart = instart - windowSize
		}
		h.reset()
		h.warmup(in, windowStart, inend)
		for i := windowStart; i < instart; i++ {
			h.update(in, i, inend)
		}
	}
	for i := 1; i < blocksize+1; i++ {
		costs[i] = largeFloat
	}
	costs[0] = 0
	lengthArray[0] = 0
	var sublen [259]uint16
	for i := instart; i < inend; i++ {
		j := i - instart
		cachedLength := uint16(0)
		sameAtPos := 0
		if useCompleteCache {
			_, length, same, ok := s.lmc.cachedLongestMatch(i - s.blockstart)
			if !ok {
				panic("zopfli: complete longest-match cache entry missing")
			}
			cachedLength = length
			sameAtPos = int(same)
		} else {
			h.update(in, i, inend)
			sameAtPos = int(h.same[i&windowMask])
		}
		if sameAtPos > maxMatch*2 && i > instart+maxMatch+1 && i+maxMatch*2+1 < inend {
			sameBefore := 0
			if useCompleteCache {
				_, _, same, ok := s.lmc.cachedLongestMatch(i - maxMatch - s.blockstart)
				if !ok {
					panic("zopfli: complete longest-match cache history missing")
				}
				sameBefore = int(same)
			} else {
				sameBefore = int(h.same[(i-maxMatch)&windowMask])
			}
			if sameBefore > maxMatch {
				symbolCost := maxMatchCost
				for range maxMatch {
					costs[j+maxMatch] = costs[j] + symbolCost
					lengthArray[j+maxMatch] = maxMatch
					i++
					j++
					if !useCompleteCache {
						h.update(in, i, inend)
					}
				}
				if useCompleteCache {
					_, cachedLength, _, _ = s.lmc.cachedLongestMatch(i - s.blockstart)
				}
			}
		}
		baseCost := costs[j]
		costsAtJ := costs[j:]
		lengthsAtJ := lengthArray[j:]
		if i+1 <= inend {
			newCost := literalCosts[in[i]] + baseCost
			if newCost < costsAtJ[1] {
				costsAtJ[1] = newCost
				lengthsAtJ[1] = 1
			}
		}
		if useCompleteCache {
			kend := minInt(int(cachedLength), inend-i)
			runs, ok := s.lmc.cachedSublenRuns(i - s.blockstart)
			if !ok {
				panic("zopfli: complete longest-match runs missing")
			}
			prevLength := minMatch
			for _, run := range runs.runs {
				runEnd := minInt(int(run.end), kend)
				if runEnd < prevLength {
					continue
				}
				distInt := int(run.dist)
				basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[getDistSymbol(distInt)]
				threshold := basePlusDist + lengthCostSuffixMin[prevLength]
				if runStart := firstImprovableLength(costsAtJ, prevLength, runEnd, threshold); runStart <= runEnd {
					relaxStatLengths(costsAtJ, lengthsAtJ, runStart, runEnd, basePlusDist, &lengthCosts)
				}
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		if cachedLeng, ends, dists, ok := tryGetFromLongestMatchCacheCompact(s, i, maxMatch); ok {
			kend := minInt(int(cachedLeng), inend-i)
			prevLength := 3
			for idx := range cacheLength {
				runEnd := minInt(int(ends[idx]), kend)
				if runEnd < prevLength {
					if runEnd == kend {
						break
					}
					continue
				}
				distInt := int(dists[idx])
				basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[getDistSymbol(distInt)]
				threshold := basePlusDist + lengthCostSuffixMin[prevLength]
				if runStart := firstImprovableLength(costsAtJ, prevLength, runEnd, threshold); runStart <= runEnd {
					relaxStatLengths(costsAtJ, lengthsAtJ, runStart, runEnd, basePlusDist, &lengthCosts)
				}
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		_, leng := findLongestMatch(s, h, in, i, inend, maxMatch, &sublen)
		kend := minInt(int(leng), inend-i)
		for k := minMatch; k <= kend; {
			distValue := sublen[k]
			runEnd := k
			for runEnd < kend && sublen[runEnd+1] == distValue {
				runEnd++
			}
			distInt := int(distValue)
			basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[getDistSymbol(distInt)]
			threshold := basePlusDist + lengthCostSuffixMin[k]
			if runStart := firstImprovableLength(costsAtJ, k, runEnd, threshold); runStart <= runEnd {
				relaxStatLengths(costsAtJ, lengthsAtJ, runStart, runEnd, basePlusDist, &lengthCosts)
			}
			k = runEnd + 1
		}
	}
	return costs[blocksize]
}

func getBestLengthsFixed(s *blockState, in []byte, instart, inend int, lengthArray []uint16, h *hash, costs []float64) float64 {
	blocksize := inend - instart
	useCompleteCache := s.lmc != nil && s.lmc.fullyBuilt()
	if !useCompleteCache {
		windowStart := 0
		if instart > windowSize {
			windowStart = instart - windowSize
		}
		h.reset()
		h.warmup(in, windowStart, inend)
		for i := windowStart; i < instart; i++ {
			h.update(in, i, inend)
		}
	}
	for i := 1; i < blocksize+1; i++ {
		costs[i] = largeFloat
	}
	costs[0] = 0
	lengthArray[0] = 0
	var sublen [259]uint16
	for i := instart; i < inend; i++ {
		j := i - instart
		if !useCompleteCache {
			h.update(in, i, inend)
		}
		baseCost := costs[j]
		literalCost := 9.0
		if in[i] <= 143 {
			literalCost = 8
		}
		newLiteralCost := literalCost + baseCost
		if newLiteralCost < costs[j+1] {
			costs[j+1] = newLiteralCost
			lengthArray[j+1] = 1
		}
		if useCompleteCache {
			_, cachedLength, _, ok := s.lmc.cachedLongestMatch(i - s.blockstart)
			if !ok {
				panic("zopfli: complete longest-match cache entry missing")
			}
			kend := minInt(int(cachedLength), inend-i)
			runs, ok := s.lmc.cachedSublenRuns(i - s.blockstart)
			if !ok {
				panic("zopfli: complete longest-match runs missing")
			}
			costsAtJ := costs[j:]
			lengthsAtJ := lengthArray[j:]
			prevLength := minMatch
			for _, run := range runs.runs {
				runEnd := minInt(int(run.end), kend)
				if runEnd < prevLength {
					continue
				}
				relaxFixedLengthRanges(costsAtJ, lengthsAtJ, prevLength, runEnd, baseCost, int(run.dist))
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		if cachedLeng, ends, dists, ok := tryGetFromLongestMatchCacheCompact(s, i, maxMatch); ok {
			kend := minInt(int(cachedLeng), inend-i)
			prevLength := 3
			for idx := range cacheLength {
				runEnd := minInt(int(ends[idx]), kend)
				if runEnd < prevLength {
					if runEnd == kend {
						break
					}
					continue
				}
				distValue := int(dists[idx])
				relaxFixedLengthRanges(costs[j:], lengthArray[j:], prevLength, runEnd, baseCost, distValue)
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		_, leng := findLongestMatch(s, h, in, i, inend, maxMatch, &sublen)
		kend := minInt(int(leng), inend-i)
		for k := minMatch; k <= kend; {
			distValue := int(sublen[k])
			runEnd := k
			for runEnd < kend && sublen[runEnd+1] == sublen[k] {
				runEnd++
			}
			relaxFixedLengthRanges(costs[j:], lengthArray[j:], k, runEnd, baseCost, distValue)
			k = runEnd + 1
		}
	}
	return costs[blocksize]
}

func traceBackwards(size int, lengthArray []uint16, path *[]uint16) {
	if size == 0 {
		return
	}
	*path = (*path)[:0]
	for index := size; ; {
		*path = append(*path, lengthArray[index])
		index -= int(lengthArray[index])
		if index == 0 {
			break
		}
	}
	for i := 0; i < len(*path)/2; i++ {
		j := len(*path) - i - 1
		(*path)[i], (*path)[j] = (*path)[j], (*path)[i]
	}
}

func followPath(s *blockState, in []byte, instart, inend int, path []uint16, store *lz77Store, h *hash) {
	store.reserveTokens(store.size + len(path))
	if s.lmc != nil && s.lmc.fullyBuilt() {
		pos := instart
		for _, length := range path {
			if length >= minMatch {
				dist, ok := s.lmc.cachedDistanceForLength(pos-s.blockstart, int(length))
				if !ok {
					panic("zopfli: incomplete longest-match cache during traceback")
				}
				verifyLenDist(in, inend, pos, dist, length)
				store.storeLitLenDist(length, dist, pos)
			} else {
				length = 1
				store.storeLitLenDist(uint16(in[pos]), 0, pos)
			}
			pos += int(length)
		}
		return
	}
	windowStart := 0
	if instart > windowSize {
		windowStart = instart - windowSize
	}
	h.reset()
	h.warmup(in, windowStart, inend)
	for i := windowStart; i < instart; i++ {
		h.update(in, i, inend)
	}
	pos := instart
	for _, length := range path {
		h.update(in, pos, inend)
		if length >= minMatch {
			dist, _ := findLongestMatch(s, h, in, pos, inend, int(length), nil)
			verifyLenDist(in, inend, pos, dist, length)
			store.storeLitLenDist(length, dist, pos)
		} else {
			length = 1
			store.storeLitLenDist(uint16(in[pos]), 0, pos)
		}
		for j := 1; j < int(length); j++ {
			h.update(in, pos+j, inend)
		}
		pos += int(length)
	}
}

func calculateStatistics(stats *symbolStats) {
	calculateEntropy(stats.litlens[:], numLL, stats.llSymbols[:])
	calculateEntropy(stats.dists[:], numD, stats.dSymbols[:])
}

func getStatistics(store *lz77Store, stats *symbolStats) {
	store.histogram(0, store.size, stats.litlens[:], stats.dists[:])
	stats.litlens[256] = 1
	calculateStatistics(stats)
}

func lz77OptimalRunStat(s *blockState, in []byte, instart, inend int, path *[]uint16, lengthArray []uint16, stats *symbolStats, store *lz77Store, h *hash, costs []float64) float64 {
	cost := getBestLengthsStat(s, in, instart, inend, stats, lengthArray, h, costs)
	traceBackwards(inend-instart, lengthArray, path)
	followPath(s, in, instart, inend, *path, store, h)
	return cost
}

func lz77OptimalRunFixed(s *blockState, in []byte, instart, inend int, path *[]uint16, lengthArray []uint16, store *lz77Store, h *hash, costs []float64) float64 {
	cost := getBestLengthsFixed(s, in, instart, inend, lengthArray, h, costs)
	traceBackwards(inend-instart, lengthArray, path)
	followPath(s, in, instart, inend, *path, store, h)
	return cost
}

func lz77OptimalWithScratch(s *blockState, in []byte, instart, inend, numIterations int, store *lz77Store, scratch *compressionScratch) {
	blocksize := inend - instart
	var lengthArray []uint16
	var pathStorage []uint16
	var path *[]uint16
	var costs []float64
	if scratch != nil {
		lengthArray, path, costs = scratch.optimalBuffers(blocksize)
	} else {
		lengthArray = make([]uint16, blocksize+1)
		pathStorage = make([]uint16, 0, blocksize/2+1)
		path = &pathStorage
		costs = make([]float64, blocksize+1)
	}
	var currentStore lz77Store
	currentStore.init(in)
	var h *hash
	if scratch != nil {
		h = &scratch.hash
	} else {
		h = &hash{}
	}
	if s.lmc != nil {
		buildLongestMatchCache(s, in, instart, inend, h)
	} else {
		h.alloc()
	}
	var stats, bestStats, lastStats symbolStats
	stats.init()
	bestCost := largeFloat
	lastCost := 0.0
	var ran ranState
	ran.init()
	lastrandomstep := -1
	lz77Greedy(s, in, instart, inend, &currentStore, h)
	getStatistics(&currentStore, &stats)
	for i := range numIterations {
		currentStore.reset()
		lz77OptimalRunStat(s, in, instart, inend, path, lengthArray, &stats, &currentStore, h, costs)
		cost := calculateBlockSizeWithScratch(&currentStore, 0, currentStore.size, 2, huffmanScratchFromCompressionScratch(scratch))
		if s.options != nil && (s.options.VerboseMore || (s.options.Verbose && cost < bestCost)) {
			fmt.Fprintf(os.Stderr, "Iteration %d: %d bit\n", i, int(cost))
		}
		improved := cost < bestCost
		if improved {
			bestStats.copyFrom(&stats)
			bestCost = cost
		}
		lastStats.copyFrom(&stats)
		getStatistics(&currentStore, &stats)
		if lastrandomstep != -1 {
			addWeighedStatFreqs(&stats, 1.0, &lastStats, 0.5, &stats)
			calculateStatistics(&stats)
		}
		if i > 5 && cost == lastCost {
			stats.copyFrom(&bestStats)
			randomizeStatFreqs(&ran, &stats)
			calculateStatistics(&stats)
			lastrandomstep = i
		}
		lastCost = cost
		if improved {
			// Keep the candidate as the best parse and reuse the displaced
			// store's buffers for the next iteration.
			*store, currentStore = currentStore, *store
			currentStore.data = in
		}
	}
}

func lz77OptimalFixedWithScratch(s *blockState, in []byte, instart, inend int, store *lz77Store, scratch *compressionScratch) {
	blocksize := inend - instart
	var lengthArray []uint16
	var pathStorage []uint16
	var path *[]uint16
	var costs []float64
	if scratch != nil {
		lengthArray, path, costs = scratch.optimalBuffers(blocksize)
	} else {
		lengthArray = make([]uint16, blocksize+1)
		pathStorage = make([]uint16, 0, blocksize/2+1)
		path = &pathStorage
		costs = make([]float64, blocksize+1)
	}
	var h *hash
	if scratch != nil {
		h = &scratch.hash
	} else {
		h = &hash{}
	}
	if s.lmc != nil {
		buildLongestMatchCache(s, in, instart, inend, h)
	} else {
		h.alloc()
	}
	s.blockstart = instart
	s.blockend = inend
	lz77OptimalRunFixed(s, in, instart, inend, path, lengthArray, store, h, costs)
}

func huffmanScratchFromCompressionScratch(scratch *compressionScratch) *huffmanScratch {
	if scratch == nil {
		return nil
	}
	return &scratch.huffman
}
