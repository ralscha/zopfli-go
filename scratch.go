package zopfli

type huffmanScratch struct {
	leaves     []node
	poolNodes  []node
	goodForRLE []bool

	llCounts  []int
	dCounts   []int
	llCounts2 []int
	dCounts2  []int

	llLengths  []uint32
	dLengths   []uint32
	llLengths2 []uint32
	dLengths2  []uint32
	llSymbols  []uint32
	dSymbols   []uint32
}

type compressionScratch struct {
	huffman huffmanScratch
	hash    hash
	lmc     longestMatchCache
}

func resizeInts(buf []int, n int) []int {
	if cap(buf) < n {
		return make([]int, n)
	}
	buf = buf[:n]
	clear(buf)
	return buf
}

func resizeUint32s(buf []uint32, n int) []uint32 {
	if cap(buf) < n {
		return make([]uint32, n)
	}
	return buf[:n]
}

func resizeBools(buf []bool, n int) []bool {
	if cap(buf) < n {
		return make([]bool, n)
	}
	buf = buf[:n]
	clear(buf)
	return buf
}

func (s *huffmanScratch) leavesBuffer(n int) []node {
	if cap(s.leaves) < n {
		s.leaves = make([]node, 0, n)
	}
	return s.leaves[:0]
}

func (s *huffmanScratch) poolBuffer(n int) []node {
	if cap(s.poolNodes) < n {
		s.poolNodes = make([]node, n)
	} else {
		s.poolNodes = s.poolNodes[:n]
	}
	return s.poolNodes
}

func (s *huffmanScratch) rleFlags(length int) []bool {
	s.goodForRLE = resizeBools(s.goodForRLE, length)
	return s.goodForRLE
}

func (s *huffmanScratch) histogramBuffers() ([]int, []int) {
	s.llCounts = resizeInts(s.llCounts, numLL)
	s.dCounts = resizeInts(s.dCounts, numD)
	return s.llCounts, s.dCounts
}

func (s *huffmanScratch) optimizedHistogramBuffers() ([]int, []int) {
	if cap(s.llCounts2) < numLL {
		s.llCounts2 = make([]int, numLL)
	} else {
		s.llCounts2 = s.llCounts2[:numLL]
	}
	if cap(s.dCounts2) < numD {
		s.dCounts2 = make([]int, numD)
	} else {
		s.dCounts2 = s.dCounts2[:numD]
	}
	return s.llCounts2, s.dCounts2
}

func (s *huffmanScratch) lengthBuffers() ([]uint32, []uint32) {
	s.llLengths = resizeUint32s(s.llLengths, numLL)
	s.dLengths = resizeUint32s(s.dLengths, numD)
	return s.llLengths, s.dLengths
}

func (s *huffmanScratch) optimizedLengthBuffers() ([]uint32, []uint32) {
	s.llLengths2 = resizeUint32s(s.llLengths2, numLL)
	s.dLengths2 = resizeUint32s(s.dLengths2, numD)
	return s.llLengths2, s.dLengths2
}

func (s *huffmanScratch) symbolBuffers() ([]uint32, []uint32) {
	s.llSymbols = resizeUint32s(s.llSymbols, numLL)
	s.dSymbols = resizeUint32s(s.dSymbols, numD)
	return s.llSymbols, s.dSymbols
}
