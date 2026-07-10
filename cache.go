package zopfli

type longestMatchCache struct {
	built             bool
	runBudgetExceeded bool
	runBudget         int
	length            []uint16
	dist              []uint16
	same              []uint16
	runCount          []uint16
	runStart          []uint32
	runs              []sublenRun
	// The legacy compact accessor reuses these 32 bytes instead of retaining
	// 32 bytes of inline run storage for every input position.
	compactEnds  [cacheLength]uint16
	compactDists [cacheLength]uint16
}

type sublenRun struct {
	end  uint16
	dist uint16
}

type sublenRunView struct {
	runs []sublenRun
}

const (
	cachedRunsPerInputByte = 12 // Twelve 4-byte runs preserve the former 60-byte/input worst-case bound.
	// Invalid DEFLATE match lengths encode cache state without separate flag slices.
	cacheLengthNoMatch    = uint16(0)
	cacheLengthUnfilled   = uint16(1)
	cacheLengthIncomplete = uint16(2)
)

func (lmc *longestMatchCache) init(blocksize int) {
	lmc.initWithRunBudget(blocksize, runBudgetForBlock(blocksize))
}

func runBudgetForBlock(blocksize int) int {
	if blocksize <= 0 {
		return 0
	}
	maxIntValue := int(^uint(0) >> 1)
	if blocksize > maxIntValue/cachedRunsPerInputByte {
		return maxIntValue
	}
	return blocksize * cachedRunsPerInputByte
}

func (lmc *longestMatchCache) initWithRunBudget(blocksize, runBudget int) {
	lmc.built = false
	lmc.runBudgetExceeded = false
	lmc.runBudget = maxInt(0, runBudget)
	if cap(lmc.length) < blocksize {
		lmc.length = make([]uint16, blocksize)
	} else {
		lmc.length = lmc.length[:blocksize]
	}
	if cap(lmc.dist) < blocksize {
		lmc.dist = make([]uint16, blocksize)
	} else {
		lmc.dist = lmc.dist[:blocksize]
	}
	if cap(lmc.same) < blocksize {
		lmc.same = make([]uint16, blocksize)
	} else {
		lmc.same = lmc.same[:blocksize]
	}
	if cap(lmc.runCount) < blocksize {
		lmc.runCount = make([]uint16, blocksize)
	} else {
		lmc.runCount = lmc.runCount[:blocksize]
	}
	if cap(lmc.runStart) < blocksize {
		lmc.runStart = make([]uint32, blocksize)
	} else {
		lmc.runStart = lmc.runStart[:blocksize]
	}
	for i := range lmc.length {
		lmc.length[i] = cacheLengthUnfilled
	}
	if cap(lmc.runs) > lmc.runBudget {
		lmc.runs = lmc.runs[:0:lmc.runBudget]
	} else {
		lmc.runs = lmc.runs[:0]
	}
}

func (lmc *longestMatchCache) fullyBuilt() bool {
	return lmc != nil && lmc.built
}

func (lmc *longestMatchCache) entryFilled(pos int) bool {
	return lmc.length[pos] != cacheLengthUnfilled
}

func (lmc *longestMatchCache) entryComplete(pos int) bool {
	length := lmc.length[pos]
	return length != cacheLengthUnfilled && length != cacheLengthIncomplete
}

func fillUint16s(dst []uint16, value uint16) {
	if len(dst) == 0 {
		return
	}
	dst[0] = value
	for filled := 1; filled < len(dst); {
		filled += copy(dst[filled:], dst[:filled])
	}
}

func (v sublenRunView) count() int {
	return len(v.runs)
}

func (v sublenRunView) at(index int) (uint16, uint16) {
	run := v.runs[index]
	return run.end, run.dist
}

func (lmc *longestMatchCache) ensureRunCapacity(required int) bool {
	if required > lmc.runBudget {
		lmc.runBudgetExceeded = true
		return false
	}
	if cap(lmc.runs) >= required {
		return true
	}
	newCapacity := cap(lmc.runs) * 2
	if newCapacity < 64 {
		newCapacity = minInt(64, lmc.runBudget)
	}
	if newCapacity < required {
		newCapacity = required
	}
	if newCapacity > lmc.runBudget {
		newCapacity = lmc.runBudget
	}
	resized := make([]sublenRun, len(lmc.runs), newCapacity)
	copy(resized, lmc.runs)
	lmc.runs = resized
	return true
}

func (lmc *longestMatchCache) storeEmptyRuns(pos int) {
	lmc.runCount[pos] = 0
	lmc.runStart[pos] = toUint32(len(lmc.runs))
}

func (lmc *longestMatchCache) storeRunsFromSublen(pos int, sublen *[maxMatch + 1]uint16, length int) bool {
	start := len(lmc.runs)
	for i := minMatch; i <= length; i++ {
		if i == length || sublen[i] != sublen[i+1] {
			if len(lmc.runs) == cap(lmc.runs) && !lmc.ensureRunCapacity(len(lmc.runs)+1) {
				lmc.runs = lmc.runs[:start]
				lmc.storeEmptyRuns(pos)
				return false
			}
			lmc.runs = append(lmc.runs, sublenRun{end: toUint16(i), dist: sublen[i]})
		}
	}
	count := len(lmc.runs) - start
	lmc.runCount[pos] = toUint16(count)
	lmc.runStart[pos] = toUint32(start)
	return count != 0
}

func (lmc *longestMatchCache) runView(pos int) sublenRunView {
	count := int(lmc.runCount[pos])
	start := int(lmc.runStart[pos])
	return sublenRunView{runs: lmc.runs[start : start+count]}
}

func (lmc *longestMatchCache) storeMatch(pos int, distance, length, same uint16, sublen *[maxMatch + 1]uint16, complete bool) bool {
	if lmc.entryFilled(pos) {
		return lmc.entryComplete(pos)
	}
	if length < minMatch {
		length = cacheLengthNoMatch
		distance = 0
	}
	runsStored := false
	if complete && length >= minMatch {
		if sublen == nil {
			complete = false
		} else {
			complete = lmc.storeRunsFromSublen(pos, sublen, int(length))
			runsStored = true
		}
	}
	if !runsStored {
		lmc.storeEmptyRuns(pos)
	}
	lmc.dist[pos] = distance
	lmc.same[pos] = same
	if complete {
		lmc.length[pos] = length
	} else {
		lmc.length[pos] = cacheLengthIncomplete
	}
	return complete
}

func (lmc *longestMatchCache) cachedLongestMatch(pos int) (uint16, uint16, uint16, bool) {
	if !lmc.entryComplete(pos) {
		return 0, 0, 0, false
	}
	return lmc.dist[pos], lmc.length[pos], lmc.same[pos], true
}

func (lmc *longestMatchCache) cachedSublenRuns(pos int) (sublenRunView, bool) {
	if !lmc.entryComplete(pos) {
		return sublenRunView{}, false
	}
	return lmc.runView(pos), true
}

func (lmc *longestMatchCache) cachedDistanceForLength(pos, length int) (uint16, bool) {
	if length < minMatch || !lmc.entryComplete(pos) || length > int(lmc.length[pos]) {
		return 0, false
	}
	if length == int(lmc.length[pos]) {
		return lmc.dist[pos], true
	}
	runs := lmc.runView(pos)
	for i := 0; i < runs.count(); i++ {
		end, distance := runs.at(i)
		if length <= int(end) {
			return distance, true
		}
	}
	return 0, false
}

func (lmc *longestMatchCache) cacheToSublen(pos, length int, sublen *[maxMatch + 1]uint16) {
	if length < minMatch {
		return
	}
	runs := lmc.runView(pos)
	prevLength := minMatch
	for i := 0; i < runs.count(); i++ {
		end, dist := runs.at(i)
		currLength := min(int(end), length)
		if currLength >= prevLength {
			fillUint16s(sublen[prevLength:currLength+1], dist)
		}
		if currLength == length {
			break
		}
		prevLength = currLength + 1
	}
}

func (lmc *longestMatchCache) compactSublen(pos, length int) (int, []uint16, []uint16, bool) {
	count := int(lmc.runCount[pos])
	if length < minMatch || !lmc.entryComplete(pos) || count == 0 || count > cacheLength {
		return 0, nil, nil, false
	}
	runs := lmc.runView(pos)
	maxLength := minInt(int(runs.runs[count-1].end), length)
	for i := range count {
		lmc.compactEnds[i] = runs.runs[i].end
		lmc.compactDists[i] = runs.runs[i].dist
	}
	for i := count; i < cacheLength; i++ {
		lmc.compactEnds[i] = toUint16(maxLength)
		lmc.compactDists[i] = 0
	}
	return maxLength, lmc.compactEnds[:], lmc.compactDists[:], true
}
