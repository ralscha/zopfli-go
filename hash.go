package zopfli

const (
	hashShift = 5
	hashMask  = 32767
	hashSize  = hashMask + 1
)

type hash struct {
	head    []int32
	prev    []uint16
	hashval []int32
	val     int

	head2    []int32
	prev2    []uint16
	hashval2 []int32
	val2     int

	same []uint16
}

func (h *hash) alloc() {
	if cap(h.head) < hashSize {
		h.head = make([]int32, hashSize)
	} else {
		h.head = h.head[:hashSize]
	}
	if cap(h.prev) < windowSize {
		h.prev = make([]uint16, windowSize)
	} else {
		h.prev = h.prev[:windowSize]
	}
	if cap(h.hashval) < windowSize {
		h.hashval = make([]int32, windowSize)
	} else {
		h.hashval = h.hashval[:windowSize]
	}
	if cap(h.same) < windowSize {
		h.same = make([]uint16, windowSize)
	} else {
		h.same = h.same[:windowSize]
	}
	if cap(h.head2) < hashSize {
		h.head2 = make([]int32, hashSize)
	} else {
		h.head2 = h.head2[:hashSize]
	}
	if cap(h.prev2) < windowSize {
		h.prev2 = make([]uint16, windowSize)
	} else {
		h.prev2 = h.prev2[:windowSize]
	}
	if cap(h.hashval2) < windowSize {
		h.hashval2 = make([]int32, windowSize)
	} else {
		h.hashval2 = h.hashval2[:windowSize]
	}
}

func (h *hash) reset() {
	h.val = 0
	for i := range h.head {
		h.head[i] = -1
		h.head2[i] = -1
	}
	for i := range windowSize {
		h.prev[i] = uint16(i)
		h.hashval[i] = -1
		h.same[i] = 0
		h.prev2[i] = uint16(i)
		h.hashval2[i] = -1
	}
	h.val2 = 0
}

func updateHashValue(h *hash, c byte) {
	h.val = ((h.val << hashShift) ^ int(c)) & hashMask
}

func (h *hash) update(array []byte, pos, end int) {
	hpos := pos & windowMask
	amount := 0
	val := h.val
	if pos+minMatch <= end {
		val = ((val << hashShift) ^ int(array[pos+minMatch-1])) & hashMask
	} else {
		val = (val << hashShift) & hashMask
	}
	h.val = val
	head := h.head
	hashval := h.hashval
	prev := h.prev
	currentVal := int32(val)
	hashval[hpos] = currentVal
	if headPos := head[val]; headPos != -1 && hashval[headPos] == currentVal {
		prev[hpos] = toUint16FromInt32(headPos)
	} else {
		prev[hpos] = toUint16(hpos)
	}
	head[val] = toInt32(hpos)

	same := h.same
	if same[(pos-1)&windowMask] > 1 {
		amount = int(same[(pos-1)&windowMask]) - 1
	}
	for pos+amount+1 < end && array[pos] == array[pos+amount+1] && amount < 0xffff {
		amount++
	}
	sameAtPos := uint16(amount)
	same[hpos] = sameAtPos

	val2 := ((int(sameAtPos) - minMatch) & 255) ^ val
	h.val2 = val2
	head2 := h.head2
	hashval2 := h.hashval2
	prev2 := h.prev2
	currentVal2 := toInt32(val2)
	hashval2[hpos] = currentVal2
	if headPos := head2[val2]; headPos != -1 && hashval2[headPos] == currentVal2 {
		prev2[hpos] = toUint16FromInt32(headPos)
	} else {
		prev2[hpos] = toUint16(hpos)
	}
	head2[val2] = toInt32(hpos)
}

func (h *hash) warmup(array []byte, pos, end int) {
	updateHashValue(h, array[pos])
	if pos+1 < end {
		updateHashValue(h, array[pos+1])
	}
}
