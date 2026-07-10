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

var distSymbolsTable = [30]int{1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577}

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

func getCostStat(litlen, dist uint16, stats *symbolStats) float64 {
	if dist == 0 {
		return stats.llSymbols[litlen]
	}
	lSym := getLengthSymbol(int(litlen))
	dSym := getDistSymbol(int(dist))
	return float64(getLengthExtraBits(int(litlen))+getDistExtraBits(int(dist))) + stats.llSymbols[lSym] + stats.dSymbols[dSym]
}

func getCostModelMinCostStat(stats *symbolStats) float64 {
	minCost := largeFloat
	bestLength := 0
	bestDist := 0
	for i := 3; i < 259; i++ {
		c := getCostStat(uint16(i), 1, stats)
		if c < minCost {
			bestLength = i
			minCost = c
		}
	}
	minCost = largeFloat
	for _, dist := range distSymbolsTable {
		c := getCostStat(3, toUint16(dist), stats)
		if c < minCost {
			bestDist = dist
			minCost = c
		}
	}
	return getCostStat(toUint16(bestLength), toUint16(bestDist), stats)
}

func relaxFixedLengthRanges(costs []float64, lengths []uint16, start, end int, baseCost float64, distValue int, minCostAdd float64) {
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
		for k := start; k <= rangeEnd; k++ {
			if costs[k] <= minCostAdd {
				continue
			}
			if newCost < costs[k] {
				costs[k] = newCost
				lengths[k] = toUint16(k)
			}
		}
		start = rangeEnd + 1
	}
}

func getBestLengthsStat(s *blockState, in []byte, instart, inend int, stats *symbolStats, lengthArray []uint16, h *hash, costs []float64) float64 {
	blocksize := inend - instart
	useCompleteCache := s.lmc != nil && s.lmc.fullyBuilt()
	minCost := getCostModelMinCostStat(stats)
	llSymbols := stats.llSymbols[:]
	dSymbols := stats.dSymbols[:]
	var literalCosts [256]float64
	copy(literalCosts[:], llSymbols[:256])
	var lengthCosts [maxMatch + 1]float64
	for length := 3; length <= maxMatch; length++ {
		lengthCosts[length] = float64(getLengthExtraBits(length)) + llSymbols[getLengthSymbol(length)]
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
			minCostAdd := minCost + baseCost
			prevLength := minMatch
			for idx := 0; idx < runs.count(); idx++ {
				end, distance := runs.at(idx)
				runEnd := minInt(int(end), kend)
				if runEnd < prevLength {
					continue
				}
				runStart := prevLength
				for runStart <= runEnd && costsAtJ[runStart] <= minCostAdd {
					runStart++
				}
				if runStart <= runEnd {
					distInt := int(distance)
					distSymbol := getDistSymbol(distInt)
					basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[distSymbol]
					k := runStart
					for ; k+3 <= runEnd; k += 4 {
						cost0 := basePlusDist + lengthCosts[k]
						if cost0 < costsAtJ[k] {
							costsAtJ[k] = cost0
							lengthsAtJ[k] = toUint16(k)
						}
						cost1 := basePlusDist + lengthCosts[k+1]
						if cost1 < costsAtJ[k+1] {
							costsAtJ[k+1] = cost1
							lengthsAtJ[k+1] = uint16(k + 1)
						}
						cost2 := basePlusDist + lengthCosts[k+2]
						if cost2 < costsAtJ[k+2] {
							costsAtJ[k+2] = cost2
							lengthsAtJ[k+2] = uint16(k + 2)
						}
						cost3 := basePlusDist + lengthCosts[k+3]
						if cost3 < costsAtJ[k+3] {
							costsAtJ[k+3] = cost3
							lengthsAtJ[k+3] = uint16(k + 3)
						}
					}
					for ; k <= runEnd; k++ {
						newCost := basePlusDist + lengthCosts[k]
						if newCost < costsAtJ[k] {
							costsAtJ[k] = newCost
							lengthsAtJ[k] = toUint16(k)
						}
					}
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
			minCostAdd := minCost + baseCost
			prevLength := 3
			for idx := range cacheLength {
				runEnd := minInt(int(ends[idx]), kend)
				if runEnd < prevLength {
					if runEnd == kend {
						break
					}
					continue
				}
				runStart := prevLength
				for runStart <= runEnd && costsAtJ[runStart] <= minCostAdd {
					runStart++
				}
				if runStart <= runEnd {
					distInt := int(dists[idx])
					distSymbol := getDistSymbol(distInt)
					basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[distSymbol]
					for k := runStart; k <= runEnd; k++ {
						newCost := basePlusDist + lengthCosts[k]
						if newCost < costsAtJ[k] {
							costsAtJ[k] = newCost
							lengthsAtJ[k] = toUint16(k)
						}
					}
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
		minCostAdd := minCost + baseCost
		for k := 3; k <= kend; {
			for k <= kend && costsAtJ[k] <= minCostAdd {
				k++
			}
			if k > kend {
				break
			}
			distValue := sublen[k]
			runEnd := k + 1
			for runEnd <= kend && sublen[runEnd] == distValue {
				runEnd++
			}
			distInt := int(distValue)
			distSymbol := getDistSymbol(distInt)
			basePlusDist := baseCost + float64(getDistExtraBits(distInt)) + dSymbols[distSymbol]
			for ; k < runEnd; k++ {
				newCost := basePlusDist + lengthCosts[k]
				if newCost < costsAtJ[k] {
					costsAtJ[k] = newCost
					lengthsAtJ[k] = toUint16(k)
				}
			}
		}
	}
	return costs[blocksize]
}

func getBestLengthsFixed(s *blockState, in []byte, instart, inend int, lengthArray []uint16, h *hash, costs []float64) float64 {
	blocksize := inend - instart
	useCompleteCache := s.lmc != nil && s.lmc.fullyBuilt()
	const minCost = 12.0
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
			minCostAdd := minCost + baseCost
			prevLength := minMatch
			for idx := 0; idx < runs.count(); idx++ {
				end, distance := runs.at(idx)
				runEnd := minInt(int(end), kend)
				if runEnd < prevLength {
					continue
				}
				distValue := int(distance)
				relaxFixedLengthRanges(costs[j:], lengthArray[j:], prevLength, runEnd, baseCost, distValue, minCostAdd)
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		if cachedLeng, ends, dists, ok := tryGetFromLongestMatchCacheCompact(s, i, maxMatch); ok {
			kend := minInt(int(cachedLeng), inend-i)
			minCostAdd := minCost + baseCost
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
				relaxFixedLengthRanges(costs[j:], lengthArray[j:], prevLength, runEnd, baseCost, distValue, minCostAdd)
				if runEnd == kend {
					break
				}
				prevLength = runEnd + 1
			}
			continue
		}
		_, leng := findLongestMatch(s, h, in, i, inend, maxMatch, &sublen)
		kend := minInt(int(leng), inend-i)
		minCostAdd := minCost + baseCost
		for k := minMatch; k <= kend; {
			distValue := int(sublen[k])
			runEnd := k
			for runEnd < kend && sublen[runEnd+1] == sublen[k] {
				runEnd++
			}
			relaxFixedLengthRanges(costs[j:], lengthArray[j:], k, runEnd, baseCost, distValue, minCostAdd)
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
