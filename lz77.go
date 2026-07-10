package zopfli

import (
	"encoding/binary"
	"fmt"
)

type lz77Store struct {
	litlens  []uint16
	dists    []uint16
	data     []byte
	pos      []int
	llSymbol []uint16
	dSymbol  []uint16
	llCounts []int
	dCounts  []int
	size     int
}

const (
	lz77StoreDensitySampleBytes = 4096
	lz77StoreMaxReservedTokens  = 256 * 1024
)

type blockState struct {
	options    *Options
	lmc        *longestMatchCache
	blockstart int
	blockend   int
}

func (s *lz77Store) init(data []byte) {
	s.litlens = nil
	s.dists = nil
	s.data = data
	s.pos = nil
	s.llSymbol = nil
	s.dSymbol = nil
	s.llCounts = nil
	s.dCounts = nil
	s.size = 0
}

func (s *lz77Store) reset() {
	s.litlens = s.litlens[:0]
	s.dists = s.dists[:0]
	s.pos = s.pos[:0]
	s.llSymbol = s.llSymbol[:0]
	s.dSymbol = s.dSymbol[:0]
	s.llCounts = s.llCounts[:0]
	s.dCounts = s.dCounts[:0]
	s.size = 0
}

func reserveUint16Capacity(values []uint16, capacity int) []uint16 {
	if cap(values) >= capacity {
		return values
	}
	result := make([]uint16, len(values), capacity)
	copy(result, values)
	return result
}

func reserveIntCapacity(values []int, capacity int) []int {
	if cap(values) >= capacity {
		return values
	}
	result := make([]int, len(values), capacity)
	copy(result, values)
	return result
}

func roundUpToMultiple(value, multiple int) int {
	if value == 0 {
		return 0
	}
	return value + multiple - 1 - (value-1)%multiple
}

func (s *lz77Store) reserveTokens(capacity int) {
	if capacity <= s.size {
		return
	}
	s.litlens = reserveUint16Capacity(s.litlens, capacity)
	s.dists = reserveUint16Capacity(s.dists, capacity)
	s.pos = reserveIntCapacity(s.pos, capacity)
	s.llSymbol = reserveUint16Capacity(s.llSymbol, capacity)
	s.dSymbol = reserveUint16Capacity(s.dSymbol, capacity)
	s.llCounts = reserveIntCapacity(s.llCounts, roundUpToMultiple(capacity, numLL))
	s.dCounts = reserveIntCapacity(s.dCounts, roundUpToMultiple(capacity, numD))
}

func literalHeavyStoreReserve(blockSize, consumed, tokenCount int) int {
	if consumed < lz77StoreDensitySampleBytes || tokenCount < consumed-consumed/4 {
		return 0
	}
	return minInt(blockSize, lz77StoreMaxReservedTokens)
}

func (s *lz77Store) ensureHistogramChunk(origSize int) {
	if origSize%numLL == 0 {
		if origSize == 0 {
			s.llCounts = append(s.llCounts, make([]int, numLL)...)
		} else {
			s.llCounts = append(s.llCounts, s.llCounts[origSize-numLL:origSize]...)
		}
	}
	if origSize%numD == 0 {
		if origSize == 0 {
			s.dCounts = append(s.dCounts, make([]int, numD)...)
		} else {
			s.dCounts = append(s.dCounts, s.dCounts[origSize-numD:origSize]...)
		}
	}
}

func (s *lz77Store) storeLitLenDist(length, dist uint16, pos int) {
	origSize := s.size
	llStart := numLL * (origSize / numLL)
	dStart := numD * (origSize / numD)
	s.ensureHistogramChunk(origSize)
	s.litlens = append(s.litlens, length)
	s.dists = append(s.dists, dist)
	s.pos = append(s.pos, pos)
	if dist == 0 {
		s.llSymbol = append(s.llSymbol, length)
		s.dSymbol = append(s.dSymbol, 0)
		s.llCounts[llStart+int(length)]++
	} else {
		llSymbol := toUint16(getLengthSymbol(int(length)))
		dSymbol := toUint16(getDistSymbol(int(dist)))
		s.llSymbol = append(s.llSymbol, llSymbol)
		s.dSymbol = append(s.dSymbol, dSymbol)
		s.llCounts[llStart+int(llSymbol)]++
		s.dCounts[dStart+int(dSymbol)]++
	}
	s.size++
}

func (s *lz77Store) appendStore(other *lz77Store) {
	for i := 0; i < other.size; i++ {
		s.storeLitLenDist(other.litlens[i], other.dists[i], other.pos[i])
	}
}

func (s *lz77Store) byteRange(lstart, lend int) int {
	if lstart == lend {
		return 0
	}
	l := lend - 1
	if s.dists[l] == 0 {
		return s.pos[l] + 1 - s.pos[lstart]
	}
	return s.pos[l] + int(s.litlens[l]) - s.pos[lstart]
}

func (s *lz77Store) histogramAt(lpos int, llCounts, dCounts []int) {
	llPos := numLL * (lpos / numLL)
	dPos := numD * (lpos / numD)
	copy(llCounts, s.llCounts[llPos:llPos+numLL])
	for i := lpos + 1; i < minInt(llPos+numLL, s.size); i++ {
		llCounts[s.llSymbol[i]]--
	}
	copy(dCounts, s.dCounts[dPos:dPos+numD])
	for i := lpos + 1; i < minInt(dPos+numD, s.size); i++ {
		if s.dists[i] != 0 {
			dCounts[s.dSymbol[i]]--
		}
	}
}

func (s *lz77Store) histogram(lstart, lend int, llCounts, dCounts []int) {
	if lstart+numLL*3 > lend {
		for i := range llCounts {
			llCounts[i] = 0
		}
		for i := range dCounts {
			dCounts[i] = 0
		}
		for i := lstart; i < lend; i++ {
			llCounts[s.llSymbol[i]]++
			if s.dists[i] != 0 {
				dCounts[s.dSymbol[i]]++
			}
		}
		return
	}
	s.histogramAt(lend-1, llCounts, dCounts)
	if lstart > 0 {
		var llCounts2 [numLL]int
		var dCounts2 [numD]int
		s.histogramAt(lstart-1, llCounts2[:], dCounts2[:])
		for i := range numLL {
			llCounts[i] -= llCounts2[i]
		}
		for i := range numD {
			dCounts[i] -= dCounts2[i]
		}
	}
}

func (b *blockState) initWithCache(options *Options, blockstart, blockend int, addLMC bool) {
	b.options = options
	b.blockstart = blockstart
	b.blockend = blockend
	if addLMC {
		b.lmc = &longestMatchCache{}
		b.lmc.init(blockend - blockstart)
	} else {
		b.lmc = nil
	}
}

// initForOptimalParse attaches an unbuilt cache. The optimal parser builds it
// immediately, avoiding the redundant sentinel fill performed by initWithCache.
func (b *blockState) initForOptimalParse(options *Options, blockstart, blockend int, lmc *longestMatchCache) {
	b.options = options
	b.blockstart = blockstart
	b.blockend = blockend
	if lmc == nil {
		lmc = &longestMatchCache{}
	}
	b.lmc = lmc
}

func getLengthScore(length, distance int) int {
	if distance > 1024 {
		return length - 1
	}
	return length
}

func verifyLenDist(data []byte, datasize, pos int, dist, length uint16) {
	if !enableAssertions {
		return
	}
	must(pos+int(length) <= datasize, "invalid length")
	for i := 0; i < int(length); i++ {
		must(data[pos-int(dist)+i] == data[pos+i], fmt.Sprintf("length/dist mismatch at %d", pos))
	}
}

func getMatch(array []byte, scan, match, end int) int {
	safeEnd := end - 8
	for scan < safeEnd && binary.LittleEndian.Uint64(array[scan:]) == binary.LittleEndian.Uint64(array[match:]) {
		scan += 8
		match += 8
	}
	for scan != end && array[scan] == array[match] {
		scan++
		match++
	}
	return scan
}

func tryGetFromLongestMatchCache(s *blockState, pos int, limit *int, sublen *[maxMatch + 1]uint16) (uint16, uint16, bool) {
	if s.lmc == nil {
		return 0, 0, false
	}
	lmcPos := pos - s.blockstart
	lmc := s.lmc
	matchDist, cachedLength, _, ok := lmc.cachedLongestMatch(lmcPos)
	if !ok {
		return 0, 0, false
	}
	matchLength := int(cachedLength)
	if matchLength == 0 {
		return 0, 0, true
	}
	if *limit != maxMatch && matchLength > *limit && sublen == nil {
		return 0, 0, false
	}
	length := toUint16(matchLength)
	if int(length) > *limit {
		length = toUint16(*limit)
	}
	if sublen == nil {
		return matchDist, length, true
	}
	lmc.cacheToSublen(lmcPos, int(length), sublen)
	distance, ok := lmc.cachedDistanceForLength(lmcPos, int(length))
	if !ok {
		return 0, 0, false
	}
	return distance, length, true
}

func tryGetFromLongestMatchCacheCompact(s *blockState, pos, limit int) (uint16, []uint16, []uint16, bool) {
	if s.lmc == nil {
		return 0, nil, nil, false
	}
	lmcPos := pos - s.blockstart
	lmc := s.lmc
	_, cachedLength, _, complete := lmc.cachedLongestMatch(lmcPos)
	if !complete {
		return 0, nil, nil, false
	}
	matchLength := int(cachedLength)
	maxCached, ends, dists, ok := lmc.compactSublen(lmcPos, matchLength)
	if !ok || matchLength > maxCached {
		return 0, nil, nil, false
	}
	if matchLength > limit {
		matchLength = limit
	}
	return toUint16(matchLength), ends, dists, true
}

func storeInLongestMatchCache(s *blockState, pos int, fullSearch bool, sublen *[maxMatch + 1]uint16, distance, length, same uint16) {
	if s.lmc == nil || !fullSearch || sublen == nil {
		return
	}
	lmcPos := pos - s.blockstart
	s.lmc.storeMatch(lmcPos, distance, length, same, sublen, true)
}

func findLongestMatch(s *blockState, h *hash, array []byte, pos, size, limit int, sublen *[maxMatch + 1]uint16) (uint16, uint16) {
	fullSearch := limit == maxMatch
	hpos := pos & windowMask
	bestDist := uint16(0)
	bestLength := uint16(1)
	if distance, length, ok := tryGetFromLongestMatchCache(s, pos, &limit, sublen); ok {
		return distance, length
	}
	sameAtPos := int(h.same[hpos])
	if size-pos < minMatch {
		if s.lmc != nil && fullSearch {
			s.lmc.storeMatch(pos-s.blockstart, 0, 0, toUint16(sameAtPos), nil, true)
		}
		return 0, 0
	}
	if pos+limit > size {
		limit = size - pos
	}
	posLimit := pos + limit
	hprev := h.prev
	usingSecondary := false
	pp := int(h.head[h.val])
	p := int(hprev[pp])
	dist := 0
	if p < pp {
		dist = pp - p
	} else {
		dist = (windowSize - p) + pp
	}
	chainCounter := maxChainHits
	for dist < windowSize {
		currentLength := 0
		if dist > 0 {
			scan := pos
			match := pos - dist
			bestLengthInt := int(bestLength)
			if pos+bestLengthInt >= size || array[scan+bestLengthInt] == array[match+bestLengthInt] {
				same0 := sameAtPos
				if same0 > 2 && array[scan] == array[match] {
					same1 := int(h.same[(pos-dist)&windowMask])
					same := min(min(same1, same0), limit)
					scan += same
					match += same
				}
				scan = getMatch(array, scan, match, posLimit)
				currentLength = scan - pos
			}
			if currentLength > int(bestLength) {
				if sublen != nil {
					fillUint16s(sublen[int(bestLength)+1:currentLength+1], toUint16(dist))
				}
				bestDist = toUint16(dist)
				bestLength = toUint16(currentLength)
				if currentLength >= limit {
					break
				}
			}
		}
		if !usingSecondary && int(bestLength) >= sameAtPos && toInt32(h.val2) == h.hashval2[p] {
			usingSecondary = true
			hprev = h.prev2
		}
		pp = p
		p = int(hprev[p])
		if p == pp {
			break
		}
		if p < pp {
			dist += pp - p
		} else {
			dist += (windowSize - p) + pp
		}
		chainCounter--
		if chainCounter <= 0 {
			break
		}
	}
	storeInLongestMatchCache(s, pos, fullSearch, sublen, bestDist, bestLength, toUint16(sameAtPos))
	return bestDist, bestLength
}

func buildLongestMatchCache(s *blockState, in []byte, instart, inend int, h *hash) {
	buildLongestMatchCacheWithRunBudget(s, in, instart, inend, h, runBudgetForBlock(inend-instart))
}

func buildLongestMatchCacheWithRunBudget(s *blockState, in []byte, instart, inend int, h *hash, runBudget int) {
	if s.lmc == nil {
		s.lmc = &longestMatchCache{}
	}
	s.blockstart = instart
	s.blockend = inend
	s.lmc.initWithRunBudget(inend-instart, runBudget)
	if instart == inend {
		s.lmc.built = true
		return
	}
	if h == nil {
		h = &hash{}
	}
	h.alloc()
	windowStart := 0
	if instart > windowSize {
		windowStart = instart - windowSize
	}
	h.reset()
	h.warmup(in, windowStart, inend)
	for i := windowStart; i < instart; i++ {
		h.update(in, i, inend)
	}
	var sublen [maxMatch + 1]uint16
	allComplete := true
	for i := instart; i < inend; i++ {
		h.update(in, i, inend)
		findLongestMatch(s, h, in, i, inend, maxMatch, &sublen)
		if s.lmc.runBudgetExceeded {
			return
		}
		if !s.lmc.entryComplete(i - instart) {
			allComplete = false
		}
	}
	s.lmc.built = allComplete
}

func lz77Greedy(s *blockState, in []byte, instart, inend int, store *lz77Store, h *hash) {
	if instart == inend {
		return
	}
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
	var prevLength uint16
	var prevMatch uint16
	var sublen [maxMatch + 1]uint16
	var cacheSublen *[maxMatch + 1]uint16
	if s.lmc != nil && !useCompleteCache {
		cacheSublen = &sublen
	}
	blockSize := inend - instart
	storeStart := store.size
	store.reserveTokens(storeStart + minInt(blockSize, lz77StoreDensitySampleBytes))
	nextDensityCheck := lz77StoreDensitySampleBytes
	reservedForLiterals := false
	matchAvailable := false
	for i := instart; i < inend; i++ {
		consumed := i - instart
		if !reservedForLiterals && consumed >= nextDensityCheck {
			reserve := literalHeavyStoreReserve(blockSize, consumed, store.size-storeStart)
			switch {
			case reserve != 0:
				store.reserveTokens(storeStart + reserve)
				reservedForLiterals = true
			case nextDensityCheck <= blockSize/2:
				nextDensityCheck *= 2
			default:
				nextDensityCheck = blockSize
			}
		}
		var dist, leng uint16
		if useCompleteCache {
			var ok bool
			dist, leng, _, ok = s.lmc.cachedLongestMatch(i - s.blockstart)
			must(ok, "complete longest-match cache entry missing")
		} else {
			h.update(in, i, inend)
			dist, leng = findLongestMatch(s, h, in, i, inend, maxMatch, cacheSublen)
		}
		lengthScore := getLengthScore(int(leng), int(dist))
		prevLengthScore := getLengthScore(int(prevLength), int(prevMatch))
		if matchAvailable {
			matchAvailable = false
			if lengthScore > prevLengthScore+1 {
				store.storeLitLenDist(uint16(in[i-1]), 0, i-1)
				if lengthScore >= minMatch && leng < maxMatch {
					matchAvailable = true
					prevLength = leng
					prevMatch = dist
					continue
				}
			} else {
				leng = prevLength
				dist = prevMatch
				verifyLenDist(in, inend, i-1, dist, leng)
				store.storeLitLenDist(leng, dist, i-1)
				for j := 2; j < int(leng); j++ {
					i++
					if !useCompleteCache {
						h.update(in, i, inend)
					}
				}
				continue
			}
		} else if lengthScore >= minMatch && leng < maxMatch {
			matchAvailable = true
			prevLength = leng
			prevMatch = dist
			continue
		}
		if lengthScore >= minMatch {
			verifyLenDist(in, inend, i, dist, leng)
			store.storeLitLenDist(leng, dist, i)
		} else {
			leng = 1
			store.storeLitLenDist(uint16(in[i]), 0, i)
		}
		for j := 1; j < int(leng); j++ {
			i++
			if !useCompleteCache {
				h.update(in, i, inend)
			}
		}
	}
}
