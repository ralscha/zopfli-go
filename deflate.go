package zopfli

type bitWriter struct {
	out []byte
	bp  uint8
}

var codeLengthOrder = [...]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

func newBitWriter(capHint int) bitWriter {
	return bitWriter{out: make([]byte, 0, capHint)}
}

func (w *bitWriter) bytes() []byte {
	return w.out
}

func (w *bitWriter) addByte(b byte) {
	w.out = append(w.out, b)
}

func (w *bitWriter) addBit(bit uint32) {
	if w.bp == 0 {
		w.out = append(w.out, 0)
	}
	w.out[len(w.out)-1] |= lowByteFromUint32(bit << w.bp)
	w.bp = (w.bp + 1) & 7
}

func (w *bitWriter) addBits(symbol uint32, length uint32) {
	for i := range length {
		bit := (symbol >> i) & 1
		if w.bp == 0 {
			w.out = append(w.out, 0)
		}
		w.out[len(w.out)-1] |= lowByteFromUint32(bit << w.bp)
		w.bp = (w.bp + 1) & 7
	}
}

func (w *bitWriter) addHuffmanBits(symbol uint32, length uint32) {
	for i := range length {
		bit := (symbol >> (length - i - 1)) & 1
		if w.bp == 0 {
			w.out = append(w.out, 0)
		}
		w.out[len(w.out)-1] |= lowByteFromUint32(bit << w.bp)
		w.bp = (w.bp + 1) & 7
	}
}

func patchDistanceCodesForBuggyDecoders(dLengths []uint32) {
	numDistCodes := 0
	for i := range 30 {
		if dLengths[i] != 0 {
			numDistCodes++
		}
		if numDistCodes >= 2 {
			return
		}
	}
	switch numDistCodes {
	case 0:
		dLengths[0], dLengths[1] = 1, 1
	case 1:
		if dLengths[0] != 0 {
			dLengths[1] = 1
		} else {
			dLengths[0] = 1
		}
	}
}

func encodeTree(llLengths, dLengths []uint32, use16, use17, use18 bool, scratch *huffmanScratch, w *bitWriter) int {
	hlit := 29
	hdist := 29
	for hlit > 0 && llLengths[257+hlit-1] == 0 {
		hlit--
	}
	for hdist > 0 && dLengths[1+hdist-1] == 0 {
		hdist--
	}
	hlit2 := hlit + 257
	lldTotal := hlit2 + hdist + 1
	var rleBuf [320]uint32
	var rleBitsBuf [320]uint32
	rle := rleBuf[:0]
	rleBits := rleBitsBuf[:0]
	var clCounts [19]int
	for i := 0; i < lldTotal; i++ {
		symbol := uint32(0)
		if i < hlit2 {
			symbol = llLengths[i]
		} else {
			symbol = dLengths[i-hlit2]
		}
		count := 1
		if use16 || (symbol == 0 && (use17 || use18)) {
			for j := i + 1; j < lldTotal; j++ {
				var next uint32
				if j < hlit2 {
					next = llLengths[j]
				} else {
					next = dLengths[j-hlit2]
				}
				if next != symbol {
					break
				}
				count++
			}
		}
		i += count - 1
		if symbol == 0 && count >= 3 {
			if use18 {
				for count >= 11 {
					count2 := min(count, 138)
					rle = append(rle, 18)
					rleBits = append(rleBits, toUint32(count2-11))
					clCounts[18]++
					count -= count2
				}
			}
			if use17 {
				for count >= 3 {
					count2 := min(count, 10)
					rle = append(rle, 17)
					rleBits = append(rleBits, toUint32(count2-3))
					clCounts[17]++
					count -= count2
				}
			}
		}
		if use16 && count >= 4 {
			count--
			clCounts[symbol]++
			rle = append(rle, symbol)
			rleBits = append(rleBits, 0)
			for count >= 3 {
				count2 := min(count, 6)
				rle = append(rle, 16)
				rleBits = append(rleBits, toUint32(count2-3))
				clCounts[16]++
				count -= count2
			}
		}
		clCounts[symbol] += count
		for count > 0 {
			rle = append(rle, symbol)
			rleBits = append(rleBits, 0)
			count--
		}
	}
	var clcl [19]uint32
	calculateBitLengths(clCounts[:], 19, 7, clcl[:], scratch)
	var clSymbols [19]uint32
	lengthsToSymbols(clcl[:], 7, clSymbols[:])
	hclen := 15
	for hclen > 0 && clCounts[codeLengthOrder[hclen+4-1]] == 0 {
		hclen--
	}
	if w != nil {
		w.addBits(uint32(hlit), 5)
		w.addBits(uint32(hdist), 5)
		w.addBits(uint32(hclen), 4)
		for i := 0; i < hclen+4; i++ {
			w.addBits(clcl[codeLengthOrder[i]], 3)
		}
		for i, symbol := range rle {
			w.addHuffmanBits(clSymbols[symbol], clcl[symbol])
			switch symbol {
			case 16:
				w.addBits(rleBits[i], 2)
			case 17:
				w.addBits(rleBits[i], 3)
			case 18:
				w.addBits(rleBits[i], 7)
			}
		}
	}
	result := 14 + (hclen+4)*3
	for i := range 19 {
		result += int(clcl[i]) * clCounts[i]
	}
	result += clCounts[16] * 2
	result += clCounts[17] * 3
	result += clCounts[18] * 7
	return result
}

func addDynamicTree(llLengths, dLengths []uint32, scratch *huffmanScratch, w *bitWriter) {
	best := 0
	bestSize := 0
	for i := range 8 {
		size := encodeTree(llLengths, dLengths, i&1 != 0, i&2 != 0, i&4 != 0, scratch, nil)
		if bestSize == 0 || size < bestSize {
			bestSize = size
			best = i
		}
	}
	encodeTree(llLengths, dLengths, best&1 != 0, best&2 != 0, best&4 != 0, scratch, w)
}

func calculateTreeSize(llLengths, dLengths []uint32, scratch *huffmanScratch) int {
	result := 0
	for i := range 8 {
		size := encodeTree(llLengths, dLengths, i&1 != 0, i&2 != 0, i&4 != 0, scratch, nil)
		if result == 0 || size < result {
			result = size
		}
	}
	return result
}

func addLZ77Data(lz77 *lz77Store, lstart, lend, expectedDataSize int, llSymbols, llLengths, dSymbols, dLengths []uint32, w *bitWriter) {
	testLength := 0
	for i := lstart; i < lend; i++ {
		dist := int(lz77.dists[i])
		litlen := int(lz77.litlens[i])
		if dist == 0 {
			w.addHuffmanBits(llSymbols[litlen], llLengths[litlen])
			testLength++
		} else {
			lls := getLengthSymbol(litlen)
			ds := getDistSymbol(dist)
			w.addHuffmanBits(llSymbols[lls], llLengths[lls])
			w.addBits(toUint32(getLengthExtraBitsValue(litlen)), toUint32(getLengthExtraBits(litlen)))
			w.addHuffmanBits(dSymbols[ds], dLengths[ds])
			w.addBits(toUint32(getDistExtraBitsValue(dist)), toUint32(getDistExtraBits(dist)))
			testLength += litlen
		}
	}
	_ = expectedDataSize
	_ = testLength
}

func getFixedTree(llLengths, dLengths []uint32) {
	for i := range 144 {
		llLengths[i] = 8
	}
	for i := 144; i < 256; i++ {
		llLengths[i] = 9
	}
	for i := 256; i < 280; i++ {
		llLengths[i] = 7
	}
	for i := 280; i < 288; i++ {
		llLengths[i] = 8
	}
	for i := range 32 {
		dLengths[i] = 5
	}
}

func calculateBlockSymbolSizeSmall(llLengths, dLengths []uint32, lz77 *lz77Store, lstart, lend int) int {
	result := 0
	for i := lstart; i < lend; i++ {
		if lz77.dists[i] == 0 {
			result += int(llLengths[lz77.litlens[i]])
		} else {
			llSymbol := getLengthSymbol(int(lz77.litlens[i]))
			dSymbol := getDistSymbol(int(lz77.dists[i]))
			result += int(llLengths[llSymbol])
			result += int(dLengths[dSymbol])
			result += getLengthSymbolExtraBits(llSymbol)
			result += getDistSymbolExtraBits(dSymbol)
		}
	}
	result += int(llLengths[256])
	return result
}

func calculateBlockSymbolSizeGivenCounts(llCounts, dCounts []int, llLengths, dLengths []uint32, lz77 *lz77Store, lstart, lend int) int {
	if lstart+numLL*3 > lend {
		return calculateBlockSymbolSizeSmall(llLengths, dLengths, lz77, lstart, lend)
	}
	result := 0
	for i := range 256 {
		result += int(llLengths[i]) * llCounts[i]
	}
	for i := 257; i < 286; i++ {
		result += int(llLengths[i]) * llCounts[i]
		result += getLengthSymbolExtraBits(i) * llCounts[i]
	}
	for i := range 30 {
		result += int(dLengths[i]) * dCounts[i]
		result += getDistSymbolExtraBits(i) * dCounts[i]
	}
	result += int(llLengths[256])
	return result
}

func calculateBlockSymbolSize(llLengths, dLengths []uint32, lz77 *lz77Store, lstart, lend int) int {
	if lstart+numLL*3 > lend {
		return calculateBlockSymbolSizeSmall(llLengths, dLengths, lz77, lstart, lend)
	}
	llCounts := make([]int, numLL)
	dCounts := make([]int, numD)
	lz77.histogram(lstart, lend, llCounts, dCounts)
	return calculateBlockSymbolSizeGivenCounts(llCounts, dCounts, llLengths, dLengths, lz77, lstart, lend)
}

func optimizeHuffmanForRLE(length int, counts []int, scratch *huffmanScratch) {
	for length >= 0 {
		if length == 0 {
			return
		}
		if counts[length-1] != 0 {
			break
		}
		length--
	}
	var goodForRLE []bool
	if scratch != nil {
		goodForRLE = scratch.rleFlags(length)
	} else {
		goodForRLE = make([]bool, length)
	}
	symbol := counts[0]
	stride := 0
	for i := 0; i < length+1; i++ {
		if i == length || counts[i] != symbol {
			if (symbol == 0 && stride >= 5) || (symbol != 0 && stride >= 7) {
				for k := 0; k < stride; k++ {
					goodForRLE[i-k-1] = true
				}
			}
			stride = 1
			if i != length {
				symbol = counts[i]
			}
		} else {
			stride++
		}
	}
	stride = 0
	limit := counts[0]
	sum := 0
	for i := 0; i < length+1; i++ {
		if i == length || goodForRLE[i] || absDiff(counts[i], limit) >= 4 {
			if stride >= 4 || (stride >= 3 && sum == 0) {
				count := max((sum+stride/2)/stride, 1)
				if sum == 0 {
					count = 0
				}
				for k := 0; k < stride; k++ {
					counts[i-k-1] = count
				}
			}
			stride = 0
			sum = 0
			switch {
			case i < length-3:
				limit = (counts[i] + counts[i+1] + counts[i+2] + counts[i+3] + 2) / 4
			case i < length:
				limit = counts[i]
			default:
				limit = 0
			}
		}
		stride++
		if i != length {
			sum += counts[i]
		}
	}
}

func tryOptimizeHuffmanForRLE(lz77 *lz77Store, lstart, lend int, llCounts, dCounts []int, llLengths, dLengths []uint32, scratch *huffmanScratch) float64 {
	treeSize := float64(calculateTreeSize(llLengths, dLengths, scratch))
	dataSize := float64(calculateBlockSymbolSizeGivenCounts(llCounts, dCounts, llLengths, dLengths, lz77, lstart, lend))
	var llCounts2, dCounts2 []int
	var llLengths2, dLengths2 []uint32
	if scratch != nil {
		llCounts2, dCounts2 = scratch.optimizedHistogramBuffers()
		llLengths2, dLengths2 = scratch.optimizedLengthBuffers()
		copy(llCounts2, llCounts)
		copy(dCounts2, dCounts)
	} else {
		llCounts2 = append([]int(nil), llCounts...)
		dCounts2 = append([]int(nil), dCounts...)
		llLengths2 = make([]uint32, numLL)
		dLengths2 = make([]uint32, numD)
	}
	optimizeHuffmanForRLE(numLL, llCounts2, scratch)
	optimizeHuffmanForRLE(numD, dCounts2, scratch)
	calculateBitLengths(llCounts2, numLL, 15, llLengths2, scratch)
	calculateBitLengths(dCounts2, numD, 15, dLengths2, scratch)
	patchDistanceCodesForBuggyDecoders(dLengths2)
	treeSize2 := float64(calculateTreeSize(llLengths2, dLengths2, scratch))
	dataSize2 := float64(calculateBlockSymbolSizeGivenCounts(llCounts, dCounts, llLengths2, dLengths2, lz77, lstart, lend))
	if treeSize2+dataSize2 < treeSize+dataSize {
		copy(llLengths, llLengths2)
		copy(dLengths, dLengths2)
		return treeSize2 + dataSize2
	}
	return treeSize + dataSize
}

func getDynamicLengthsWithScratch(lz77 *lz77Store, lstart, lend int, llLengths, dLengths []uint32, scratch *huffmanScratch) float64 {
	var llCounts, dCounts []int
	if scratch != nil {
		llCounts, dCounts = scratch.histogramBuffers()
	} else {
		llCounts = make([]int, numLL)
		dCounts = make([]int, numD)
	}
	lz77.histogram(lstart, lend, llCounts, dCounts)
	llCounts[256] = 1
	calculateBitLengths(llCounts, numLL, 15, llLengths, scratch)
	calculateBitLengths(dCounts, numD, 15, dLengths, scratch)
	patchDistanceCodesForBuggyDecoders(dLengths)
	return tryOptimizeHuffmanForRLE(lz77, lstart, lend, llCounts, dCounts, llLengths, dLengths, scratch)
}

func calculateBlockSizeWithScratch(lz77 *lz77Store, lstart, lend, btype int, scratch *huffmanScratch) float64 {
	var llLengths, dLengths []uint32
	if scratch != nil {
		llLengths, dLengths = scratch.lengthBuffers()
	} else {
		llLengths = make([]uint32, numLL)
		dLengths = make([]uint32, numD)
	}
	result := 3.0
	if btype == 0 {
		length := lz77.byteRange(lstart, lend)
		rem := length % 65535
		blocks := length / 65535
		if rem != 0 {
			blocks++
		}
		return float64(blocks*5*8 + length*8)
	}
	if btype == 1 {
		getFixedTree(llLengths, dLengths)
		result += float64(calculateBlockSymbolSize(llLengths, dLengths, lz77, lstart, lend))
	} else {
		result += getDynamicLengthsWithScratch(lz77, lstart, lend, llLengths, dLengths, scratch)
	}
	return result
}

func calculateBlockSizeAutoTypeWithScratch(lz77 *lz77Store, lstart, lend int, scratch *huffmanScratch) float64 {
	uncompressedCost := calculateBlockSizeWithScratch(lz77, lstart, lend, 0, scratch)
	fixedCost := uncompressedCost
	if lz77.size <= 1000 {
		fixedCost = calculateBlockSizeWithScratch(lz77, lstart, lend, 1, scratch)
	}
	dynCost := calculateBlockSizeWithScratch(lz77, lstart, lend, 2, scratch)
	switch {
	case uncompressedCost < fixedCost && uncompressedCost < dynCost:
		return uncompressedCost
	case fixedCost < dynCost:
		return fixedCost
	default:
		return dynCost
	}
}

func addNonCompressedBlock(options *Options, final bool, in []byte, instart, inend int, w *bitWriter) {
	_ = options
	pos := instart
	for {
		blocksize := 65535
		if pos+blocksize > inend {
			blocksize = inend - pos
		}
		currentFinal := pos+blocksize >= inend
		nlen := ^uint16(blocksize)
		if final && currentFinal {
			w.addBit(1)
		} else {
			w.addBit(0)
		}
		w.addBit(0)
		w.addBit(0)
		w.bp = 0
		w.addByte(lowByteFromInt(blocksize))
		w.addByte(lowByteFromInt(blocksize / 256))
		w.addByte(lowByteFromInt(int(nlen)))
		w.addByte(lowByteFromInt(int(nlen) / 256))
		w.out = append(w.out, in[pos:pos+blocksize]...)
		if currentFinal {
			break
		}
		pos += blocksize
	}
}

func addLZ77BlockWithScratch(options *Options, btype int, final bool, lz77 *lz77Store, lstart, lend, expectedDataSize int, scratch *huffmanScratch, w *bitWriter) {
	var llLengths, dLengths []uint32
	var llSymbols, dSymbols []uint32
	if scratch != nil {
		llLengths, dLengths = scratch.lengthBuffers()
		llSymbols, dSymbols = scratch.symbolBuffers()
	} else {
		llLengths = make([]uint32, numLL)
		dLengths = make([]uint32, numD)
		llSymbols = make([]uint32, numLL)
		dSymbols = make([]uint32, numD)
	}
	if btype == 0 {
		length := lz77.byteRange(lstart, lend)
		pos := 0
		if lstart != lend {
			pos = lz77.pos[lstart]
		}
		addNonCompressedBlock(options, final, lz77.data, pos, pos+length, w)
		return
	}
	if final {
		w.addBit(1)
	} else {
		w.addBit(0)
	}
	w.addBit(uint32(btype & 1))
	w.addBit(uint32((btype & 2) >> 1))
	if btype == 1 {
		getFixedTree(llLengths, dLengths)
	} else {
		getDynamicLengthsWithScratch(lz77, lstart, lend, llLengths, dLengths, scratch)
		addDynamicTree(llLengths, dLengths, scratch, w)
	}
	lengthsToSymbols(llLengths, 15, llSymbols)
	lengthsToSymbols(dLengths, 15, dSymbols)
	addLZ77Data(lz77, lstart, lend, expectedDataSize, llSymbols, llLengths, dSymbols, dLengths, w)
	w.addHuffmanBits(llSymbols[256], llLengths[256])
}

func addLZ77BlockAutoTypeWithScratch(options *Options, final bool, lz77 *lz77Store, lstart, lend, expectedDataSize int, scratch *compressionScratch, w *bitWriter) {
	huffScratch := huffmanScratchFromCompressionScratch(scratch)
	uncompressedCost := calculateBlockSizeWithScratch(lz77, lstart, lend, 0, huffScratch)
	fixedCost := calculateBlockSizeWithScratch(lz77, lstart, lend, 1, huffScratch)
	dynCost := calculateBlockSizeWithScratch(lz77, lstart, lend, 2, huffScratch)
	expensiveFixed := lz77.size < 1000 || fixedCost <= dynCost*1.1
	if lstart == lend {
		if final {
			w.addBits(1, 1)
		} else {
			w.addBits(0, 1)
		}
		w.addBits(1, 2)
		w.addBits(0, 7)
		return
	}
	var fixedStore lz77Store
	fixedStore.init(lz77.data)
	if expensiveFixed {
		instart := lz77.pos[lstart]
		inend := instart + lz77.byteRange(lstart, lend)
		var s blockState
		var lmc *longestMatchCache
		if scratch != nil {
			lmc = &scratch.lmc
		}
		s.initWithCache(options, instart, inend, true, lmc)
		lz77OptimalFixedWithScratch(&s, lz77.data, instart, inend, &fixedStore, scratch)
		fixedCost = calculateBlockSizeWithScratch(&fixedStore, 0, fixedStore.size, 1, huffScratch)
	}
	switch {
	case uncompressedCost < fixedCost && uncompressedCost < dynCost:
		addLZ77BlockWithScratch(options, 0, final, lz77, lstart, lend, expectedDataSize, huffScratch, w)
	case fixedCost < dynCost:
		if expensiveFixed {
			addLZ77BlockWithScratch(options, 1, final, &fixedStore, 0, fixedStore.size, expectedDataSize, huffScratch, w)
		} else {
			addLZ77BlockWithScratch(options, 1, final, lz77, lstart, lend, expectedDataSize, huffScratch, w)
		}
	default:
		addLZ77BlockWithScratch(options, 2, final, lz77, lstart, lend, expectedDataSize, huffScratch, w)
	}
}

func deflatePart(options *Options, btype int, final bool, in []byte, instart, inend int, w *bitWriter) {
	if btype == 0 {
		addNonCompressedBlock(options, final, in, instart, inend, w)
		return
	}
	if btype == 1 {
		var store lz77Store
		store.init(in)
		var scratch compressionScratch
		var s blockState
		s.initWithCache(options, instart, inend, true, &scratch.lmc)
		lz77OptimalFixedWithScratch(&s, in, instart, inend, &store, &scratch)
		addLZ77BlockWithScratch(options, btype, final, &store, 0, store.size, 0, &scratch.huffman, w)
		return
	}
	splitpointsUncompressed := []int(nil)
	var scratch compressionScratch
	if options != nil && options.BlockSplitting {
		splitpointsUncompressed = blockSplitWithScratch(options, in, instart, inend, options.BlockSplittingMax, &scratch)
	}
	var lz77 lz77Store
	lz77.init(in)
	totalCost := 0.0
	splitPoints := make([]int, len(splitpointsUncompressed))
	for i := 0; i <= len(splitpointsUncompressed); i++ {
		start := instart
		if i > 0 {
			start = splitpointsUncompressed[i-1]
		}
		end := inend
		if i < len(splitpointsUncompressed) {
			end = splitpointsUncompressed[i]
		}
		var s blockState
		var store lz77Store
		store.init(in)
		s.initWithCache(options, start, end, true, &scratch.lmc)
		lz77OptimalWithScratch(&s, in, start, end, options.NumIterations, &store, &scratch)
		totalCost += calculateBlockSizeAutoTypeWithScratch(&store, 0, store.size, &scratch.huffman)
		lz77.appendStore(&store)
		if i < len(splitpointsUncompressed) {
			splitPoints[i] = lz77.size
		}
	}
	if options != nil && options.BlockSplitting && len(splitpointsUncompressed) > 1 {
		splitPoints2 := blockSplitLZ77WithScratch(options, &lz77, options.BlockSplittingMax, &scratch.huffman)
		totalCost2 := 0.0
		for i := 0; i <= len(splitPoints2); i++ {
			start := 0
			if i > 0 {
				start = splitPoints2[i-1]
			}
			end := lz77.size
			if i < len(splitPoints2) {
				end = splitPoints2[i]
			}
			totalCost2 += calculateBlockSizeAutoTypeWithScratch(&lz77, start, end, &scratch.huffman)
		}
		if totalCost2 < totalCost {
			splitPoints = splitPoints2
		}
	}
	for i := 0; i <= len(splitPoints); i++ {
		start := 0
		if i > 0 {
			start = splitPoints[i-1]
		}
		end := lz77.size
		if i < len(splitPoints) {
			end = splitPoints[i]
		}
		addLZ77BlockAutoTypeWithScratch(options, i == len(splitPoints) && final, &lz77, start, end, 0, &scratch, w)
	}
}

func deflate(options *Options, btype int, final bool, in []byte, w *bitWriter) {
	if len(in) == 0 {
		if final {
			w.addBits(1, 1)
		} else {
			w.addBits(0, 1)
		}
		w.addBits(1, 2)
		w.addBits(0, 7)
		return
	}
	if masterBlockSize == 0 {
		deflatePart(options, btype, final, in, 0, len(in), w)
		return
	}
	for i := 0; i < len(in); i += masterBlockSize {
		end := min(i+masterBlockSize, len(in))
		deflatePart(options, btype, final && end == len(in), in, i, end, w)
	}
}
