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

func (s *lz77Store) copyFrom(src *lz77Store) {
	s.data = src.data
	s.size = src.size
	s.litlens = append(s.litlens[:0], src.litlens...)
	s.dists = append(s.dists[:0], src.dists...)
	s.pos = append(s.pos[:0], src.pos...)
	s.llSymbol = append(s.llSymbol[:0], src.llSymbol...)
	s.dSymbol = append(s.dSymbol[:0], src.dSymbol...)
	s.llCounts = append(s.llCounts[:0], src.llCounts...)
	s.dCounts = append(s.dCounts[:0], src.dCounts...)
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

func (b *blockState) initWithCache(options *Options, blockstart, blockend int, addLMC bool, lmc *longestMatchCache) {
	b.options = options
	b.blockstart = blockstart
	b.blockend = blockend
	if addLMC {
		if lmc != nil {
			lmc.init(blockend - blockstart)
			b.lmc = lmc
		} else {
			b.lmc = &longestMatchCache{}
			b.lmc.init(blockend - blockstart)
		}
	} else {
		b.lmc = nil
	}
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
	matchLength := int(lmc.length[lmcPos])
	matchDist := lmc.dist[lmcPos]
	if matchLength != 0 && matchDist == 0 {
		return 0, 0, false
	}
	if *limit != maxMatch && matchLength > *limit {
		if sublen == nil {
			return 0, 0, false
		}
		maxCached := maxCachedSublenForCache(lmc.sublenMaxLen[lmcPos], matchLength)
		if maxCached < *limit {
			return 0, 0, false
		}
	}
	if sublen == nil {
		length := toUint16(matchLength)
		if int(length) > *limit {
			length = toUint16(*limit)
		}
		return matchDist, length, true
	}
	maxCached := maxCachedSublenForCache(lmc.sublenMaxLen[lmcPos], matchLength)
	if matchLength > maxCached {
		*limit = matchLength
		return 0, 0, false
	}
	length := toUint16(matchLength)
	if int(length) > *limit {
		length = toUint16(*limit)
	}
	lmc.cacheToSublen(lmcPos, int(length), sublen)
	return sublen[length], length, true
}

func tryGetFromLongestMatchCacheCompact(s *blockState, pos, limit int) (uint16, []uint16, []uint16, bool) {
	if s.lmc == nil {
		return 0, nil, nil, false
	}
	lmcPos := pos - s.blockstart
	lmc := s.lmc
	matchLength := int(lmc.length[lmcPos])
	matchDist := lmc.dist[lmcPos]
	if matchLength != 0 && matchDist == 0 {
		return 0, nil, nil, false
	}
	maxCached, ends, dists, ok := lmc.compactSublen(lmcPos, matchLength)
	if !ok || matchLength > maxCached {
		return 0, nil, nil, false
	}
	if matchLength > limit {
		matchLength = limit
	}
	return toUint16(matchLength), ends, dists, true
}

func storeInLongestMatchCache(s *blockState, pos, limit int, sublen *[maxMatch + 1]uint16, distance, length uint16, runs *sublenRunCollector) {
	if s.lmc == nil || limit != maxMatch || sublen == nil {
		return
	}
	lmcPos := pos - s.blockstart
	cacheAvailable := s.lmc.length[lmcPos] == 0 || s.lmc.dist[lmcPos] != 0
	if cacheAvailable {
		return
	}
	if length < minMatch {
		s.lmc.dist[lmcPos] = 0
		s.lmc.length[lmcPos] = 0
	} else {
		s.lmc.dist[lmcPos] = distance
		s.lmc.length[lmcPos] = length
	}
	if runs != nil {
		s.lmc.storeRuns(lmcPos, runs)
		return
	}
	s.lmc.sublenToCache(sublen, lmcPos, int(length))
}

func findLongestMatch(s *blockState, h *hash, array []byte, pos, size, limit int, sublen *[maxMatch + 1]uint16) (uint16, uint16) {
	hpos := pos & windowMask
	bestDist := uint16(0)
	bestLength := uint16(1)
	var runs sublenRunCollector
	useRuns := s.lmc != nil && limit == maxMatch && sublen != nil
	if useRuns {
		runs.reset()
	}
	if distance, length, ok := tryGetFromLongestMatchCache(s, pos, &limit, sublen); ok {
		return distance, length
	}
	if size-pos < minMatch {
		return 0, 0
	}
	if pos+limit > size {
		limit = size - pos
	}
	posLimit := pos + limit
	sameAtPos := int(h.same[hpos])
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
					if useRuns {
						runs.record(currentLength, toUint16(dist))
					}
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
	var runPtr *sublenRunCollector
	if useRuns {
		runPtr = &runs
	}
	storeInLongestMatchCache(s, pos, limit, sublen, bestDist, bestLength, runPtr)
	return bestDist, bestLength
}

func lz77Greedy(s *blockState, in []byte, instart, inend int, store *lz77Store, h *hash) {
	if instart == inend {
		return
	}
	windowStart := 0
	if instart > windowSize {
		windowStart = instart - windowSize
	}
	h.reset(windowSize)
	h.warmup(in, windowStart, inend)
	for i := windowStart; i < instart; i++ {
		h.update(in, i, inend)
	}
	var prevLength uint16
	var prevMatch uint16
	matchAvailable := false
	for i := instart; i < inend; i++ {
		h.update(in, i, inend)
		dist, leng := findLongestMatch(s, h, in, i, inend, maxMatch, nil)
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
					h.update(in, i, inend)
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
			h.update(in, i, inend)
		}
	}
}
