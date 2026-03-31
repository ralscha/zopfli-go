package zopfli

func lengthsToSymbols(lengths []uint32, maxBits uint32, symbols []uint32) {
	var smallBL [16]int
	var smallNext [16]uint32
	var blCount []int
	var nextCode []uint32
	if maxBits <= 15 {
		blCount = smallBL[:maxBits+1]
		nextCode = smallNext[:maxBits+1]
	} else {
		blCount = make([]int, maxBits+1)
		nextCode = make([]uint32, maxBits+1)
	}
	for i := range symbols {
		symbols[i] = 0
	}
	for _, length := range lengths {
		blCount[length]++
	}
	var code uint32
	blCount[0] = 0
	for bits := uint32(1); bits <= maxBits; bits++ {
		code = (code + uint32(blCount[bits-1])) << 1
		nextCode[bits] = code
	}
	for i, length := range lengths {
		if length != 0 {
			symbols[i] = nextCode[length]
			nextCode[length]++
		}
	}
}

func calculateEntropy(count []int, n int, bitlengths []float64) {
	sum := 0
	for i := range n {
		sum += count[i]
	}
	log2sum := 0.0
	if sum == 0 {
		log2sum = log2(float64(n))
	} else {
		log2sum = log2(float64(sum))
	}
	for i := range n {
		if count[i] == 0 {
			bitlengths[i] = log2sum
		} else {
			bitlengths[i] = log2sum - log2(float64(count[i]))
		}
		bitlengths[i] = clampNearZero(bitlengths[i])
	}
}

func calculateBitLengths(count []int, n, maxBits int, bitlengths []uint32, scratch *huffmanScratch) {
	if err := lengthLimitedCodeLengths(count, n, maxBits, bitlengths, scratch); err != nil {
		panic(err)
	}
}
