package zopfli

import (
	"errors"
	"slices"
)

type nodeIndex int

const noNode nodeIndex = -1

type node struct {
	weight int
	tail   nodeIndex
	count  int
}

type nodePool struct {
	nodes []node
	next  int
}

func (p *nodePool) newNode() nodeIndex {
	n := nodeIndex(p.next)
	p.next++
	return n
}

func initNode(weight, count int, tail nodeIndex, n *node) {
	n.weight = weight
	n.count = count
	n.tail = tail
}

func boundaryPM(lists [][2]nodeIndex, leaves []node, numSymbols int, pool *nodePool, index int) {
	lastCount := pool.nodes[lists[index][1]].count
	if index == 0 && lastCount >= numSymbols {
		return
	}
	newChain := pool.newNode()
	oldChain := lists[index][1]
	lists[index][0] = oldChain
	lists[index][1] = newChain
	if index == 0 {
		initNode(leaves[lastCount].weight, lastCount+1, noNode, &pool.nodes[newChain])
		return
	}
	sum := pool.nodes[lists[index-1][0]].weight + pool.nodes[lists[index-1][1]].weight
	if lastCount < numSymbols && sum > leaves[lastCount].weight {
		initNode(leaves[lastCount].weight, lastCount+1, pool.nodes[oldChain].tail, &pool.nodes[newChain])
	} else {
		initNode(sum, lastCount, lists[index-1][1], &pool.nodes[newChain])
		boundaryPM(lists, leaves, numSymbols, pool, index-1)
		boundaryPM(lists, leaves, numSymbols, pool, index-1)
	}
}

func boundaryPMFinal(lists [][2]nodeIndex, leaves []node, numSymbols int, pool *nodePool, index int) {
	lastChain := lists[index][1]
	lastCount := pool.nodes[lastChain].count
	sum := pool.nodes[lists[index-1][0]].weight + pool.nodes[lists[index-1][1]].weight
	if lastCount < numSymbols && sum > leaves[lastCount].weight {
		newChain := pool.newNode()
		oldChain := pool.nodes[lastChain].tail
		lists[index][1] = newChain
		pool.nodes[newChain].count = lastCount + 1
		pool.nodes[newChain].tail = oldChain
	} else {
		pool.nodes[lastChain].tail = lists[index-1][1]
	}
}

func initLists(pool *nodePool, leaves []node, maxBits int, lists [][2]nodeIndex) {
	node0 := pool.newNode()
	node1 := pool.newNode()
	initNode(leaves[0].weight, 1, noNode, &pool.nodes[node0])
	initNode(leaves[1].weight, 2, noNode, &pool.nodes[node1])
	for i := range maxBits {
		lists[i][0] = node0
		lists[i][1] = node1
	}
}

func extractBitLengths(chain nodeIndex, nodes, leaves []node, bitlengths []uint32, maxBits int) {
	var smallCounts [16]int
	counts := smallCounts[:maxBits+1]
	if maxBits+1 > len(smallCounts) {
		counts = make([]int, maxBits+1)
	}
	end := maxBits + 1
	ptr := maxBits
	value := uint32(1)
	for chain != noNode {
		n := &nodes[chain]
		end--
		counts[end] = n.count
		chain = n.tail
	}
	val := counts[maxBits]
	for ptr >= end {
		for ; ptr > 0 && val > counts[ptr-1]; val-- {
			bitlengths[leaves[val-1].count] = value
		}
		ptr--
		value++
	}
}

func fillLeaves(frequencies []int, n int, leaves []node) {
	next := 0
	for i := range n {
		if frequencies[i] != 0 {
			leaves[next] = node{weight: frequencies[i], count: i}
			next++
		}
	}
}

func compareLeaves(a, b node) int {
	if a.weight < b.weight {
		return -1
	}
	if a.weight > b.weight {
		return 1
	}
	if a.count < b.count {
		return -1
	}
	if a.count > b.count {
		return 1
	}
	return 0
}

func insertionSortLeaves(leaves []node) {
	for i := 1; i < len(leaves); i++ {
		leaf := leaves[i]
		j := i - 1
		for ; j >= 0 && compareLeaves(leaves[j], leaf) > 0; j-- {
			leaves[j+1] = leaves[j]
		}
		leaves[j+1] = leaf
	}
}

func lengthLimitedCodeLengthsSmallLeaves(frequencies []int, n, numSymbols, maxBits int, bitlengths []uint32, scratch *huffmanScratch) {
	var smallLeaves [32]node
	leaves := smallLeaves[:numSymbols]
	fillLeaves(frequencies, n, leaves)
	insertionSortLeaves(leaves)
	lengthLimitedCodeLengthsWithLeaves(leaves, maxBits, bitlengths, scratch)
}

func lengthLimitedCodeLengthsLargeLeaves(frequencies []int, n, numSymbols, maxBits int, bitlengths []uint32, scratch *huffmanScratch) {
	var leaves []node
	if scratch != nil {
		leaves = scratch.leavesBuffer(numSymbols)[:numSymbols]
	} else {
		leaves = make([]node, numSymbols)
	}
	fillLeaves(frequencies, n, leaves)
	slices.SortFunc(leaves, compareLeaves)
	lengthLimitedCodeLengthsWithLeaves(leaves, maxBits, bitlengths, scratch)
}

func lengthLimitedCodeLengthsWithLeaves(leaves []node, maxBits int, bitlengths []uint32, scratch *huffmanScratch) {
	numSymbols := len(leaves)
	if numSymbols-1 < maxBits {
		maxBits = numSymbols - 1
	}
	neededPool := maxBits * 2 * numSymbols
	if neededPool <= 64 {
		lengthLimitedCodeLengthsSmallPool(leaves, maxBits, bitlengths, neededPool)
		return
	}
	var poolNodes []node
	if scratch != nil {
		poolNodes = scratch.poolBuffer(neededPool)
	} else {
		poolNodes = make([]node, neededPool)
	}
	lengthLimitedCodeLengthsWithPool(leaves, maxBits, bitlengths, poolNodes)
}

// Keep the fixed-size pool in the small-pool call frame instead of reserving it
// in the much more common reusable-scratch path.
//
//go:noinline
func lengthLimitedCodeLengthsSmallPool(leaves []node, maxBits int, bitlengths []uint32, neededPool int) {
	var smallPoolNodes [64]node
	lengthLimitedCodeLengthsWithPool(leaves, maxBits, bitlengths, smallPoolNodes[:neededPool])
}

func lengthLimitedCodeLengthsWithPool(leaves []node, maxBits int, bitlengths []uint32, poolNodes []node) {
	pool := nodePool{nodes: poolNodes}
	var smallLists [16][2]nodeIndex
	lists := smallLists[:maxBits]
	if maxBits > len(smallLists) {
		lists = make([][2]nodeIndex, maxBits)
	}
	initLists(&pool, leaves, maxBits, lists)
	numRuns := 2*len(leaves) - 4
	for i := 0; i < numRuns-1; i++ {
		boundaryPM(lists, leaves, len(leaves), &pool, maxBits-1)
	}
	boundaryPMFinal(lists, leaves, len(leaves), &pool, maxBits-1)
	extractBitLengths(lists[maxBits-1][1], poolNodes, leaves, bitlengths, maxBits)
}

func lengthLimitedCodeLengths(frequencies []int, n, maxBits int, bitlengths []uint32, scratch *huffmanScratch) error {
	var firstSymbols [2]int
	numSymbols := 0
	for i := range n {
		bitlengths[i] = 0
		if frequencies[i] != 0 {
			if numSymbols < len(firstSymbols) {
				firstSymbols[numSymbols] = i
			}
			numSymbols++
		}
	}
	if (1 << maxBits) < numSymbols {
		return errors.New("zopfli: too few maxbits")
	}
	if numSymbols == 0 {
		return nil
	}
	if numSymbols <= 2 {
		for i := range numSymbols {
			bitlengths[firstSymbols[i]] = 1
		}
		return nil
	}
	if numSymbols <= 32 {
		lengthLimitedCodeLengthsSmallLeaves(frequencies, n, numSymbols, maxBits, bitlengths, scratch)
	} else {
		lengthLimitedCodeLengthsLargeLeaves(frequencies, n, numSymbols, maxBits, bitlengths, scratch)
	}
	return nil
}
