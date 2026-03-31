package zopfli

type Options struct {
	Verbose            bool
	VerboseMore        bool
	NumIterations      int
	BlockSplitting     bool
	BlockSplittingLast bool
	BlockSplittingMax  int
}

type Format int

const (
	FormatGzip Format = iota
	FormatZlib
	FormatDeflate
)

func DefaultOptions() Options {
	return Options{
		NumIterations:      15,
		BlockSplitting:     true,
		BlockSplittingLast: false,
		BlockSplittingMax:  15,
	}
}

func Compress(options *Options, outputType Format, in []byte) []byte {
	opt := DefaultOptions()
	if options != nil {
		opt = *options
	}

	switch outputType {
	case FormatGzip:
		return gzipCompress(&opt, in)
	case FormatZlib:
		return zlibCompress(&opt, in)
	case FormatDeflate:
		writer := newBitWriter(estimateOutputCap(len(in)))
		deflate(&opt, 2, true, in, &writer)
		return writer.bytes()
	default:
		panic("zopfli: unknown format")
	}
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
