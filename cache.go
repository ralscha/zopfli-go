package zopfli

type longestMatchCache struct {
	length       []uint16
	dist         []uint16
	sublenEnds   []uint16
	sublenDists  []uint16
	sublenMaxLen []uint16
}

type sublenRunCollector struct {
	count  int
	maxLen uint16
	ends   [cacheLength]uint16
	dists  [cacheLength]uint16
}

const sublenCacheStride = cacheLength

func (lmc *longestMatchCache) init(blocksize int) {
	if cap(lmc.length) < blocksize {
		lmc.length = make([]uint16, blocksize)
	} else {
		lmc.length = lmc.length[:blocksize]
	}
	if cap(lmc.dist) < blocksize {
		lmc.dist = make([]uint16, blocksize)
	} else {
		lmc.dist = lmc.dist[:blocksize]
		clear(lmc.dist)
	}
	cacheSize := sublenCacheStride * blocksize
	if cap(lmc.sublenEnds) < cacheSize {
		lmc.sublenEnds = make([]uint16, cacheSize)
	} else {
		lmc.sublenEnds = lmc.sublenEnds[:cacheSize]
		clear(lmc.sublenEnds)
	}
	if cap(lmc.sublenDists) < cacheSize {
		lmc.sublenDists = make([]uint16, cacheSize)
	} else {
		lmc.sublenDists = lmc.sublenDists[:cacheSize]
		clear(lmc.sublenDists)
	}
	if cap(lmc.sublenMaxLen) < blocksize {
		lmc.sublenMaxLen = make([]uint16, blocksize)
	} else {
		lmc.sublenMaxLen = lmc.sublenMaxLen[:blocksize]
		clear(lmc.sublenMaxLen)
	}
	for i := range lmc.length {
		lmc.length[i] = 1
	}
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

func (c *sublenRunCollector) reset() {
	c.count = 0
	c.maxLen = 0
}

func (c *sublenRunCollector) record(length int, dist uint16) {
	if c == nil || c.count >= cacheLength {
		return
	}
	c.ends[c.count] = toUint16(length)
	c.dists[c.count] = dist
	c.maxLen = toUint16(length)
	c.count++
}

func (lmc *longestMatchCache) cacheEntry(pos int) ([]uint16, []uint16) {
	base := pos * sublenCacheStride
	return lmc.sublenEnds[base : base+sublenCacheStride], lmc.sublenDists[base : base+sublenCacheStride]
}

func maxCachedSublenForCache(maxCached uint16, length int) int {
	if maxCached == 0 {
		return 0
	}
	maxLength := int(maxCached)
	if maxLength > length {
		return length
	}
	return maxLength
}

func (lmc *longestMatchCache) sublenToCache(sublen *[maxMatch + 1]uint16, pos, length int) {
	if cacheLength == 0 || length < 3 {
		return
	}
	ends, dists := lmc.cacheEntry(pos)
	j := 0
	bestLength := 0
	for i := 3; i <= length; i++ {
		if i == length || sublen[i] != sublen[i+1] {
			dist := sublen[i]
			ends[j] = uint16(i)
			dists[j] = dist
			bestLength = i
			j++
			if j >= cacheLength {
				break
			}
		}
	}
	lmc.sublenMaxLen[pos] = uint16(bestLength)
	if j < cacheLength {
		for ; j < cacheLength; j++ {
			ends[j] = uint16(bestLength)
			dists[j] = 0
		}
	}
}

func (lmc *longestMatchCache) storeRuns(pos int, runs *sublenRunCollector) {
	ends, dists := lmc.cacheEntry(pos)
	count := 0
	maxLen := uint16(0)
	if runs != nil {
		count = runs.count
		maxLen = runs.maxLen
		copy(ends, runs.ends[:count])
		copy(dists, runs.dists[:count])
	}
	lmc.sublenMaxLen[pos] = maxLen
	if count < cacheLength {
		for ; count < cacheLength; count++ {
			ends[count] = maxLen
			dists[count] = 0
		}
	}
}

func (lmc *longestMatchCache) cacheToSublen(pos, length int, sublen *[maxMatch + 1]uint16) {
	if cacheLength == 0 || length < 3 {
		return
	}
	maxLength := maxCachedSublenForCache(lmc.sublenMaxLen[pos], length)
	if maxLength == 0 {
		return
	}
	ends, dists := lmc.cacheEntry(pos)
	prevLength := 3
	for j := range cacheLength {
		currLength := min(int(ends[j]), maxLength)
		dist := dists[j]
		if currLength >= prevLength {
			fillUint16s(sublen[prevLength:currLength+1], dist)
		}
		if currLength == maxLength {
			break
		}
		prevLength = currLength + 1
	}
}

func (lmc *longestMatchCache) compactSublen(pos, length int) (int, []uint16, []uint16, bool) {
	if cacheLength == 0 || length < 3 {
		return 0, nil, nil, false
	}
	maxLength := maxCachedSublenForCache(lmc.sublenMaxLen[pos], length)
	if maxLength == 0 {
		return 0, nil, nil, false
	}
	ends, dists := lmc.cacheEntry(pos)
	return maxLength, ends, dists, true
}
