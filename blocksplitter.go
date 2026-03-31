package zopfli

import (
	"fmt"
	"os"
	"sort"
)

type splitCostContext struct {
	lz77    *lz77Store
	start   int
	end     int
	scratch *huffmanScratch
}

func findMinimum(f func(int, *splitCostContext) float64, context *splitCostContext, start, end int) (int, float64) {
	if end-start < 1024 {
		best := largeFloat
		result := start
		for i := start; i < end; i++ {
			v := f(i, context)
			if v < best {
				best = v
				result = i
			}
		}
		return result, best
	}
	const num = 9
	lastBest := largeFloat
	pos := start
	for {
		if end-start <= num {
			break
		}
		var p [num]int
		var vp [num]float64
		for i := range num {
			p[i] = start + (i+1)*((end-start)/(num+1))
			vp[i] = f(p[i], context)
		}
		bestI := 0
		best := vp[0]
		for i := 1; i < num; i++ {
			if vp[i] < best {
				best = vp[i]
				bestI = i
			}
		}
		if best > lastBest {
			break
		}
		if bestI != 0 {
			start = p[bestI-1]
		}
		if bestI != num-1 {
			end = p[bestI+1]
		}
		pos = p[bestI]
		lastBest = best
	}
	return pos, lastBest
}

func estimateCost(lz77 *lz77Store, lstart, lend int, scratch *huffmanScratch) float64 {
	return calculateBlockSizeAutoTypeWithScratch(lz77, lstart, lend, scratch)
}

func splitCost(i int, context *splitCostContext) float64 {
	return estimateCost(context.lz77, context.start, i, context.scratch) + estimateCost(context.lz77, i, context.end, context.scratch)
}

func addSorted(value int, out *[]int) {
	*out = append(*out, value)
	sort.Ints(*out)
}

func printBlockSplitPoints(lz77 *lz77Store, lz77SplitPoints []int) {
	splitPoints := make([]int, 0, len(lz77SplitPoints))
	nPoints := 0
	pos := 0
	if len(lz77SplitPoints) > 0 {
		for i := 0; i < lz77.size; i++ {
			length := 1
			if lz77.dists[i] != 0 {
				length = int(lz77.litlens[i])
			}
			if lz77SplitPoints[nPoints] == i {
				splitPoints = append(splitPoints, pos)
				nPoints++
				if nPoints == len(lz77SplitPoints) {
					break
				}
			}
			pos += length
		}
	}
	fmt.Fprint(os.Stderr, "block split points: ")
	for _, p := range splitPoints {
		fmt.Fprintf(os.Stderr, "%d ", p)
	}
	fmt.Fprint(os.Stderr, "(hex:")
	for _, p := range splitPoints {
		fmt.Fprintf(os.Stderr, " %x", p)
	}
	fmt.Fprintln(os.Stderr, ")")
}

func findLargestSplittableBlock(lz77Size int, done []byte, splitPoints []int) (int, int, bool) {
	longest := 0
	found := false
	startOut, endOut := 0, 0
	for i := 0; i <= len(splitPoints); i++ {
		start := 0
		if i != 0 {
			start = splitPoints[i-1]
		}
		end := lz77Size - 1
		if i != len(splitPoints) {
			end = splitPoints[i]
		}
		if done[start] == 0 && end-start > longest {
			startOut = start
			endOut = end
			found = true
			longest = end - start
		}
	}
	return startOut, endOut, found
}

func blockSplitLZ77WithScratch(options *Options, lz77 *lz77Store, maxBlocks int, scratch *huffmanScratch) []int {
	if lz77.size < 10 {
		return nil
	}
	done := make([]byte, lz77.size)
	lstart, lend := 0, lz77.size
	splitPoints := make([]int, 0, 8)
	numBlocks := 1
	for {
		if maxBlocks > 0 && numBlocks >= maxBlocks {
			break
		}
		ctx := &splitCostContext{lz77: lz77, start: lstart, end: lend, scratch: scratch}
		llPos, splitCost := findMinimum(splitCost, ctx, lstart+1, lend)
		origCost := estimateCost(lz77, lstart, lend, scratch)
		if splitCost > origCost || llPos == lstart+1 || llPos == lend {
			done[lstart] = 1
		} else {
			addSorted(llPos, &splitPoints)
			numBlocks++
		}
		var ok bool
		lstart, lend, ok = findLargestSplittableBlock(lz77.size, done, splitPoints)
		if !ok || lend-lstart < 10 {
			break
		}
	}
	if options != nil && options.Verbose {
		printBlockSplitPoints(lz77, splitPoints)
	}
	return splitPoints
}

func blockSplitWithScratch(options *Options, in []byte, instart, inend, maxBlocks int, scratch *compressionScratch) []int {
	var s blockState
	s.initWithCache(options, instart, inend, false, nil)
	var store lz77Store
	store.init(in)
	var h *hash
	if scratch != nil {
		h = &scratch.hash
	} else {
		h = &hash{}
	}
	h.alloc(windowSize)
	lz77Greedy(&s, in, instart, inend, &store, h)
	lz77SplitPoints := blockSplitLZ77WithScratch(options, &store, maxBlocks, huffmanScratchFromCompressionScratch(scratch))
	result := make([]int, 0, len(lz77SplitPoints))
	pos := instart
	nPoints := 0
	if len(lz77SplitPoints) > 0 {
		for i := 0; i < store.size; i++ {
			length := 1
			if store.dists[i] != 0 {
				length = int(store.litlens[i])
			}
			if lz77SplitPoints[nPoints] == i {
				result = append(result, pos)
				nPoints++
				if nPoints == len(lz77SplitPoints) {
					break
				}
			}
			pos += length
		}
	}
	return result
}
