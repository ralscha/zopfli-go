package zopfli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultOutputGolden(t *testing.T) {
	data := deterministicOptimizationFixture()
	compressed := Gzip(data)
	got := sha256.Sum256(compressed)
	const want = "7caa05e0f5be3d34435746a399b65be61257fce75771627c474b5b651f6d999d"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("default gzip SHA-256 = %s, want %s", hex.EncodeToString(got[:]), want)
	}
}

func TestFastOptions(t *testing.T) {
	options := FastOptions()
	if options.NumIterations != 3 {
		t.Fatalf("NumIterations = %d, want 3", options.NumIterations)
	}
	if !options.BlockSplitting {
		t.Fatal("fast profile unexpectedly disables block splitting")
	}
	compressed := Compress(&options, FormatGzip, deterministicOptimizationFixture())
	assertGzipRoundTrip(t, "fast profile", compressed, deterministicOptimizationFixture())
}

func TestNonPositiveIterationsUseOneIteration(t *testing.T) {
	data := []byte(strings.Repeat("iteration-boundary-normalization\n", 64))
	oneIteration := DefaultOptions()
	oneIteration.NumIterations = 1
	want := Compress(&oneIteration, FormatGzip, data)
	for _, numIterations := range []int{0, -1} {
		options := DefaultOptions()
		options.NumIterations = numIterations
		got := Compress(&options, FormatGzip, data)
		if !bytes.Equal(got, want) {
			t.Fatalf("NumIterations %d output differs from one iteration", numIterations)
		}
		assertGzipRoundTrip(t, "non-positive iterations", got, data)
		if options.NumIterations != numIterations {
			t.Fatalf("Compress mutated NumIterations: got %d, want %d", options.NumIterations, numIterations)
		}
	}
}

func TestOptionsPositionalLiteralCompatibility(t *testing.T) {
	options := Options{false, false, 1, true, false, 15}
	if options.NumIterations != 1 || !options.BlockSplitting {
		t.Fatal("positional Options literal fields changed")
	}
}

func TestParallelSplitBlocksMatchSerialOutput(t *testing.T) {
	data := parallelOptimizationFixture()
	serial := FastOptions()

	var scratch compressionScratch
	splitPoints := blockSplitWithScratch(&serial, data, 0, len(data), serial.BlockSplittingMax, &scratch)
	if len(splitPoints) == 0 {
		t.Fatal("parallel test fixture did not produce split blocks")
	}
	want := Compress(&serial, FormatGzip, data)
	got := CompressParallel(&serial, FormatGzip, data, 4)
	if !bytes.Equal(got, want) {
		t.Fatalf("parallel output differs from serial output: got %d bytes, want %d", len(got), len(want))
	}
	assertGzipRoundTrip(t, "parallel split blocks", got, data)
}

func TestParallelMasterBlocksMatchSerialOutput(t *testing.T) {
	data := []byte(strings.Repeat("master-block-parallelism-0123456789\n", 150_000))
	if len(data) <= 4*masterBlockSize {
		t.Fatalf("master block fixture is only %d bytes", len(data))
	}
	serial := FastOptions()
	serial.NumIterations = 1
	serial.BlockSplitting = false

	want := Compress(&serial, FormatGzip, data)
	got := CompressParallel(&serial, FormatGzip, data, 2)
	if !bytes.Equal(got, want) {
		t.Fatalf("parallel master-block output differs: got %d bytes, want %d", len(got), len(want))
	}
	assertGzipRoundTrip(t, "parallel master blocks", got, data)
}

func TestParallelMasterBlocksForcedTypesMatchSerialOutput(t *testing.T) {
	data := []byte(strings.Repeat("forced-master-block-type-abcdefghij\n", 60_000))
	for _, btype := range []int{0, 1} {
		t.Run([]string{"uncompressed", "fixed"}[btype], func(t *testing.T) {
			options := FastOptions()
			compress := func(numWorkers int) []byte {
				writer := newBitWriter(estimateOutputCap(len(data)))
				deflate(&options, btype, data, &writer, numWorkers)
				return writer.bytes()
			}
			want := compress(1)
			got := compress(2)
			if !bytes.Equal(got, want) {
				t.Fatalf("parallel btype %d output differs: got %d bytes, want %d", btype, len(got), len(want))
			}
			assertDeflateRoundTrip(t, "parallel forced master blocks", got, data)
		})
	}
}

func TestCompressionWorkersFallbacks(t *testing.T) {
	if got := compressionWorkers(&Options{}, 0, 8); got != 1 {
		t.Fatalf("zero requested workers = %d, want 1", got)
	}
	if got := compressionWorkers(&Options{Verbose: true}, 8, 8); got != 1 {
		t.Fatalf("verbose compression = %d workers, want 1", got)
	}
	if got := compressionWorkers(&Options{}, 8, 8); got != MaxCompressionWorkers {
		t.Fatalf("capped compression = %d workers, want %d", got, MaxCompressionWorkers)
	}
	if got := compressionWorkers(&Options{}, 8, 3); got != 3 {
		t.Fatalf("bounded compression = %d workers, want 3", got)
	}
}

func TestCachedStatisticsMatchTokenScan(t *testing.T) {
	data := parallelOptimizationFixture()
	options := DefaultOptions()
	var state blockState
	state.initWithCache(&options, 0, len(data), false)
	var store lz77Store
	store.init(data)
	var h hash
	h.alloc()
	lz77Greedy(&state, data, 0, len(data), &store, &h)

	var got symbolStats
	getStatistics(&store, &got)
	var want symbolStats
	for i := 0; i < store.size; i++ {
		if store.dists[i] == 0 {
			want.litlens[store.litlens[i]]++
		} else {
			want.litlens[getLengthSymbol(int(store.litlens[i]))]++
			want.dists[getDistSymbol(int(store.dists[i]))]++
		}
	}
	want.litlens[256] = 1
	calculateStatistics(&want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("cached statistics differ from a direct token scan")
	}
}

func TestSegmentedFixedLengthRelaxationMatchesScalar(t *testing.T) {
	fixedWant := make([]float64, maxMatch+1)
	fixedGot := make([]float64, maxMatch+1)
	fixedWantLengths := make([]uint16, maxMatch+1)
	fixedGotLengths := make([]uint16, maxMatch+1)
	for i := range fixedWant {
		fixedWant[i] = 80 + float64(i%13)
		fixedGot[i] = fixedWant[i]
	}
	baseCost := 70.5
	distance := 1234
	minCostAdd := 79.0
	for k := minMatch; k <= maxMatch; k++ {
		if fixedWant[k] <= minCostAdd {
			continue
		}
		lengthSymbol := getLengthSymbol(k)
		cost := baseCost + float64(getLengthExtraBits(k)+getDistExtraBits(distance)+5)
		if lengthSymbol <= 279 {
			cost += 7
		} else {
			cost += 8
		}
		if cost < fixedWant[k] {
			fixedWant[k] = cost
			fixedWantLengths[k] = toUint16(k)
		}
	}
	relaxFixedLengthRanges(fixedGot, fixedGotLengths, minMatch, maxMatch, baseCost, distance, minCostAdd)
	if !reflect.DeepEqual(fixedGot, fixedWant) || !reflect.DeepEqual(fixedGotLengths, fixedWantLengths) {
		t.Fatal("segmented fixed relaxation differs from scalar order")
	}
}

func deterministicOptimizationFixture() []byte {
	text := []byte(strings.Repeat("alpha beta gamma delta; alpha beta gamma epsilon\n", 512))
	data := make([]byte, 64*1024)
	copy(data, text)
	state := uint32(1)
	for i := len(text); i < len(data); i++ {
		state = state*1664525 + 1013904223
		data[i] = byte(state >> 24)
	}
	return data
}

func parallelOptimizationFixture() []byte {
	data := make([]byte, 0,
		2400*len("aaaaaaaa-bbbbbbbb-cccccccc\n")+
			1800*len("{\"name\":\"worker\",\"enabled\":true}\n")+
			64*1024+
			1600*len("<div><span>parallel zopfli block</span></div>\n"))
	data = append(data, []byte(strings.Repeat("aaaaaaaa-bbbbbbbb-cccccccc\n", 2400))...)
	data = append(data, []byte(strings.Repeat("{\"name\":\"worker\",\"enabled\":true}\n", 1800))...)
	state := uint32(7)
	for range 64 * 1024 {
		state = state*1664525 + 1013904223
		data = append(data, byte(state>>24))
	}
	data = append(data, []byte(strings.Repeat("<div><span>parallel zopfli block</span></div>\n", 1600))...)
	return data
}
