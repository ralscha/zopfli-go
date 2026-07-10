package zopfli

import (
	"slices"
	"testing"
)

func TestLengthLimitedCodeLengthsTieOrder(t *testing.T) {
	equalFrequencies := make([]int, 35)
	for i := range equalFrequencies {
		equalFrequencies[i] = 1
	}
	equalLengths := make([]uint32, 35)
	for i := range equalLengths {
		if i < 6 {
			equalLengths[i] = 6
		} else {
			equalLengths[i] = 5
		}
	}

	tests := []struct {
		name        string
		frequencies []int
		maxBits     int
		want        []uint32
	}{
		{name: "three equal", frequencies: []int{1, 1, 1}, maxBits: 15, want: []uint32{2, 2, 1}},
		{name: "five equal", frequencies: []int{1, 1, 1, 1, 1}, maxBits: 15, want: []uint32{3, 3, 2, 2, 2}},
		{name: "thirty-five equal", frequencies: equalFrequencies, maxBits: 15, want: equalLengths},
		{name: "limited", frequencies: []int{100, 50, 25, 12, 6, 3, 1, 1, 1, 1}, maxBits: 4, want: []uint32{2, 2, 4, 4, 4, 4, 4, 4, 4, 4}},
		{name: "ties and gaps", frequencies: []int{0, 8, 0, 8, 4, 4, 4, 2, 2, 0, 1}, maxBits: 5, want: []uint32{0, 2, 0, 2, 3, 3, 3, 5, 4, 0, 5}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bitlengths := make([]uint32, len(test.frequencies))
			if err := lengthLimitedCodeLengths(test.frequencies, len(test.frequencies), test.maxBits, bitlengths, nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(bitlengths, test.want) {
				t.Fatalf("bit lengths = %v, want %v", bitlengths, test.want)
			}
		})
	}
}

func TestLengthLimitedCodeLengthsScratchAllocations(t *testing.T) {
	smallFrequencies := make([]int, 19)
	for i := range smallFrequencies {
		smallFrequencies[i] = i + 1
	}
	largeFrequencies := make([]int, 35)
	for i := range largeFrequencies {
		largeFrequencies[i] = i + 1
	}

	tests := []struct {
		name        string
		frequencies []int
		maxBits     int
		scratch     *huffmanScratch
	}{
		{name: "stack leaves and pool", frequencies: []int{1, 1, 1}, maxBits: 15},
		{name: "stack leaves and scratch pool", frequencies: smallFrequencies, maxBits: 7, scratch: &huffmanScratch{}},
		{name: "scratch leaves and pool", frequencies: largeFrequencies, maxBits: 15, scratch: &huffmanScratch{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bitlengths := make([]uint32, len(test.frequencies))
			if err := lengthLimitedCodeLengths(test.frequencies, len(test.frequencies), test.maxBits, bitlengths, test.scratch); err != nil {
				t.Fatal(err)
			}
			allocations := testing.AllocsPerRun(1000, func() {
				if err := lengthLimitedCodeLengths(test.frequencies, len(test.frequencies), test.maxBits, bitlengths, test.scratch); err != nil {
					panic(err)
				}
			})
			if allocations != 0 {
				t.Fatalf("allocations per call = %v, want 0", allocations)
			}
		})
	}
}

func BenchmarkLengthLimitedCodeLengthsScratch(b *testing.B) {
	codeLengthFrequencies := make([]int, 19)
	for i := range codeLengthFrequencies {
		codeLengthFrequencies[i] = i + 1
	}
	literalLengthFrequencies := make([]int, 288)
	for i := range literalLengthFrequencies {
		literalLengthFrequencies[i] = i%17 + 1
	}

	for _, benchmark := range []struct {
		name        string
		frequencies []int
		maxBits     int
	}{
		{name: "code-length-alphabet", frequencies: codeLengthFrequencies, maxBits: 7},
		{name: "literal-length-alphabet", frequencies: literalLengthFrequencies, maxBits: 15},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			bitlengths := make([]uint32, len(benchmark.frequencies))
			scratch := &huffmanScratch{}
			if err := lengthLimitedCodeLengths(benchmark.frequencies, len(benchmark.frequencies), benchmark.maxBits, bitlengths, scratch); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := lengthLimitedCodeLengths(benchmark.frequencies, len(benchmark.frequencies), benchmark.maxBits, bitlengths, scratch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
