package zopfli

type Options struct {
	Verbose        bool
	VerboseMore    bool
	NumIterations  int
	BlockSplitting bool
	// BlockSplittingLast is retained for upstream API compatibility and has no effect.
	//
	// Deprecated: the compressor uses its hybrid splitter and may compare a
	// post-parsing split when the initial split yields multiple points.
	BlockSplittingLast bool
	BlockSplittingMax  int
}

type Format int

const (
	FormatGzip Format = iota
	FormatZlib
	FormatDeflate
)

// MaxCompressionWorkers is the maximum parallel block-analysis workers used
// by one CompressParallel call.
const MaxCompressionWorkers = 4

func DefaultOptions() Options {
	return Options{
		NumIterations:      15,
		BlockSplitting:     true,
		BlockSplittingLast: false,
		BlockSplittingMax:  15,
	}
}

// FastOptions returns a speed-oriented profile that retains block splitting
// while reducing the number of optimal parsing iterations.
func FastOptions() Options {
	options := DefaultOptions()
	options.NumIterations = 3
	return options
}

func Compress(options *Options, outputType Format, in []byte) []byte {
	return compress(options, outputType, in, 1)
}

// CompressParallel compresses in using up to numWorkers parallel block-analysis
// workers. Values below two select serial compression; values above
// MaxCompressionWorkers are capped.
func CompressParallel(options *Options, outputType Format, in []byte, numWorkers int) []byte {
	return compress(options, outputType, in, normalizeCompressionWorkers(numWorkers))
}

func compress(options *Options, outputType Format, in []byte, numWorkers int) []byte {
	opt := DefaultOptions()
	if options != nil {
		opt = *options
	}
	if opt.NumIterations <= 0 {
		opt.NumIterations = 1
	}

	switch outputType {
	case FormatGzip:
		return gzipCompress(&opt, in, numWorkers)
	case FormatZlib:
		return zlibCompress(&opt, in, numWorkers)
	case FormatDeflate:
		writer := newBitWriter(estimateOutputCap(len(in)))
		deflate(&opt, 2, in, &writer, numWorkers)
		return writer.bytes()
	default:
		panic("zopfli: unknown format")
	}
}

func normalizeCompressionWorkers(numWorkers int) int {
	if numWorkers <= 1 {
		return 1
	}
	return min(numWorkers, MaxCompressionWorkers)
}

func Gzip(in []byte) []byte {
	opt := DefaultOptions()
	return Compress(&opt, FormatGzip, in)
}

func Zlib(in []byte) []byte {
	opt := DefaultOptions()
	return Compress(&opt, FormatZlib, in)
}

func Deflate(in []byte) []byte {
	opt := DefaultOptions()
	return Compress(&opt, FormatDeflate, in)
}

func estimateOutputCap(n int) int {
	if n < 32 {
		return 64
	}
	return n/2 + 64
}
