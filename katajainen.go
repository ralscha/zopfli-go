package zopfli

import (
	"errors"
	"sort"
)

type node struct {
	weight int
	tail   *node
	count  int
}

type nodePool struct {
	nodes []node
	next  int
}

func (p *nodePool) newNode() *node {
	n := &p.nodes[p.next]
	p.next++
	return n
}

func initNode(weight, count int, tail *node, n *node) {
	n.weight = weight
	n.count = count
	n.tail = tail
}

func boundaryPM(lists [][2]*node, leaves []node, numSymbols int, pool *nodePool, index int) {
	lastCount := lists[index][1].count
	if index == 0 && lastCount >= numSymbols {
		return
	}
	newChain := pool.newNode()
	oldChain := lists[index][1]
	lists[index][0] = oldChain
	lists[index][1] = newChain
	if index == 0 {
		initNode(leaves[lastCount].weight, lastCount+1, nil, newChain)
		return
	}
	sum := lists[index-1][0].weight + lists[index-1][1].weight
	if lastCount < numSymbols && sum > leaves[lastCount].weight {
		initNode(leaves[lastCount].weight, lastCount+1, oldChain.tail, newChain)
	} else {
		initNode(sum, lastCount, lists[index-1][1], newChain)
		boundaryPM(lists, leaves, numSymbols, pool, index-1)
		boundaryPM(lists, leaves, numSymbols, pool, index-1)
	}
}

func boundaryPMFinal(lists [][2]*node, leaves []node, numSymbols int, pool *nodePool, index int) {
	lastCount := lists[index][1].count
	sum := lists[index-1][0].weight + lists[index-1][1].weight
	if lastCount < numSymbols && sum > leaves[lastCount].weight {
		newChain := pool.newNode()
		oldChain := lists[index][1].tail
		lists[index][1] = newChain
		newChain.count = lastCount + 1
		newChain.tail = oldChain
	} else {
		lists[index][1].tail = lists[index-1][1]
	}
}

func initLists(pool *nodePool, leaves []node, maxBits int, lists [][2]*node) {
	node0 := pool.newNode()
	node1 := pool.newNode()
	initNode(leaves[0].weight, 1, nil, node0)
	initNode(leaves[1].weight, 2, nil, node1)
	for i := range maxBits {
		lists[i][0] = node0
		lists[i][1] = node1
	}
}

func extractBitLengths(chain *node, leaves []node, bitlengths []uint32, maxBits int) {
	var smallCounts [16]int
	counts := smallCounts[:maxBits+1]
	if maxBits+1 > len(smallCounts) {
		counts = make([]int, maxBits+1)
	}
	end := maxBits + 1
	ptr := maxBits
	value := uint32(1)
	for n := chain; n != nil; n = n.tail {
		end--
		counts[end] = n.count
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

func lengthLimitedCodeLengths(frequencies []int, n, maxBits int, bitlengths []uint32, scratch *huffmanScratch) error {
	for i := range n {
		bitlengths[i] = 0
	}
	var smallLeaves [32]node
	leaves := smallLeaves[:0]
	if n > len(smallLeaves) {
		if scratch != nil {
			leaves = scratch.leavesBuffer(n)
		} else {
			leaves = make([]node, 0, n)
		}
	}
	for i := range n {
		if frequencies[i] != 0 {
			leaves = append(leaves, node{weight: frequencies[i], count: i})
		}
	}
	numSymbols := len(leaves)
	if (1 << maxBits) < numSymbols {
		return errors.New("zopfli: too few maxbits")
	}
	if numSymbols == 0 {
		return nil
	}
	if numSymbols == 1 {
		bitlengths[leaves[0].count] = 1
		return nil
	}
	if numSymbols == 2 {
		bitlengths[leaves[0].count]++
		bitlengths[leaves[1].count]++
		return nil
	}
	if numSymbols <= 32 {
		for i := 1; i < numSymbols; i++ {
			leaf := leaves[i]
			j := i - 1
			for ; j >= 0; j-- {
				if leaves[j].weight < leaf.weight || (leaves[j].weight == leaf.weight && leaves[j].count < leaf.count) {
					break
				}
				leaves[j+1] = leaves[j]
			}
			leaves[j+1] = leaf
		}
	} else {
		sort.Slice(leaves, func(i, j int) bool {
			if leaves[i].weight != leaves[j].weight {
				return leaves[i].weight < leaves[j].weight
			}
			return leaves[i].count < leaves[j].count
		})
	}
	if numSymbols-1 < maxBits {
		maxBits = numSymbols - 1
	}
	var smallPoolNodes [64]node
	pool := &nodePool{}
	neededPool := maxBits * 2 * numSymbols
	switch {
	case neededPool <= len(smallPoolNodes):
		pool.nodes = smallPoolNodes[:neededPool]
	case scratch != nil:
		pool.nodes = scratch.poolBuffer(neededPool)
	default:
		pool.nodes = make([]node, neededPool)
	}
	var smallLists [16][2]*node
	lists := smallLists[:maxBits]
	if maxBits > len(smallLists) {
		lists = make([][2]*node, maxBits)
	}
	initLists(pool, leaves, maxBits, lists)
	numRuns := 2*numSymbols - 4
	for i := 0; i < numRuns-1; i++ {
		boundaryPM(lists, leaves, numSymbols, pool, maxBits-1)
	}
	boundaryPMFinal(lists, leaves, numSymbols, pool, maxBits-1)
	extractBitLengths(lists[maxBits-1][1], leaves, bitlengths, maxBits)
	return nil
}
