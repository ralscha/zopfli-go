package zopfli

import "math/bits"

var (
	distSymbolTable         [windowSize + 1]uint16
	distExtraBitsTable      [windowSize + 1]uint8
	distExtraBitsValueTable [windowSize + 1]uint16
)

var lengthExtraBitsTable = [259]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 0,
}

var lengthExtraBitsValueTable = [259]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 2, 3, 0,
	1, 2, 3, 0, 1, 2, 3, 0, 1, 2, 3, 0, 1, 2, 3, 4, 5, 6, 7, 0, 1, 2, 3, 4, 5,
	6, 7, 0, 1, 2, 3, 4, 5, 6, 7, 0, 1, 2, 3, 4, 5, 6, 7, 0, 1, 2, 3, 4, 5, 6,
	7, 8, 9, 10, 11, 12, 13, 14, 15, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
	13, 14, 15, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0, 1, 2,
	3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
	10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28,
	29, 30, 31, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17,
	18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 0, 1, 2, 3, 4, 5, 6,
	7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26,
	27, 28, 29, 30, 31, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 0,
}

var lengthSymbolTable = [259]uint16{
	0, 0, 0, 257, 258, 259, 260, 261, 262, 263, 264,
	265, 265, 266, 266, 267, 267, 268, 268,
	269, 269, 269, 269, 270, 270, 270, 270,
	271, 271, 271, 271, 272, 272, 272, 272,
	273, 273, 273, 273, 273, 273, 273, 273,
	274, 274, 274, 274, 274, 274, 274, 274,
	275, 275, 275, 275, 275, 275, 275, 275,
	276, 276, 276, 276, 276, 276, 276, 276,
	277, 277, 277, 277, 277, 277, 277, 277,
	277, 277, 277, 277, 277, 277, 277, 277,
	278, 278, 278, 278, 278, 278, 278, 278,
	278, 278, 278, 278, 278, 278, 278, 278,
	279, 279, 279, 279, 279, 279, 279, 279,
	279, 279, 279, 279, 279, 279, 279, 279,
	280, 280, 280, 280, 280, 280, 280, 280,
	280, 280, 280, 280, 280, 280, 280, 280,
	281, 281, 281, 281, 281, 281, 281, 281,
	281, 281, 281, 281, 281, 281, 281, 281,
	281, 281, 281, 281, 281, 281, 281, 281,
	281, 281, 281, 281, 281, 281, 281, 281,
	282, 282, 282, 282, 282, 282, 282, 282,
	282, 282, 282, 282, 282, 282, 282, 282,
	282, 282, 282, 282, 282, 282, 282, 282,
	282, 282, 282, 282, 282, 282, 282, 282,
	283, 283, 283, 283, 283, 283, 283, 283,
	283, 283, 283, 283, 283, 283, 283, 283,
	283, 283, 283, 283, 283, 283, 283, 283,
	283, 283, 283, 283, 283, 283, 283, 283,
	284, 284, 284, 284, 284, 284, 284, 284,
	284, 284, 284, 284, 284, 284, 284, 284,
	284, 284, 284, 284, 284, 284, 284, 284,
	284, 284, 284, 284, 284, 284, 284, 285,
}

var lengthSymbolExtraBits = [29]uint8{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}
var distSymbolExtraBits = [30]uint8{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}

func init() {
	for dist := 1; dist <= windowSize; dist++ {
		symbol, extraBits, extraValue := computeDistCode(dist)
		distSymbolTable[dist] = uint16(symbol)
		distExtraBitsTable[dist] = uint8(extraBits)
		distExtraBitsValueTable[dist] = uint16(extraValue)
	}
}

func computeDistCode(dist int) (symbol, extraBits, extraValue int) {
	if dist < 5 {
		return dist - 1, 0, 0
	}
	l := bits.Len(uint(dist-1)) - 1
	r := ((dist - 1) >> (l - 1)) & 1
	symbol = l*2 + r
	extraBits = l - 1
	extraValue = (dist - (1 + (1 << l))) & ((1 << (l - 1)) - 1)
	return symbol, extraBits, extraValue
}

func getDistExtraBits(dist int) int {
	if dist <= windowSize {
		return int(distExtraBitsTable[dist])
	}
	_, extraBits, _ := computeDistCode(dist)
	return extraBits
}

func getDistExtraBitsValue(dist int) int {
	if dist <= windowSize {
		return int(distExtraBitsValueTable[dist])
	}
	_, _, extraValue := computeDistCode(dist)
	return extraValue
}

func getDistSymbol(dist int) int {
	if dist <= windowSize {
		return int(distSymbolTable[dist])
	}
	symbol, _, _ := computeDistCode(dist)
	return symbol
}

func getLengthExtraBits(length int) int {
	return int(lengthExtraBitsTable[length])
}

func getLengthExtraBitsValue(length int) int {
	return int(lengthExtraBitsValueTable[length])
}

func getLengthSymbol(length int) int {
	return int(lengthSymbolTable[length])
}

func getLengthSymbolExtraBits(symbol int) int {
	return int(lengthSymbolExtraBits[symbol-257])
}

func getDistSymbolExtraBits(symbol int) int {
	return int(distSymbolExtraBits[symbol])
}
