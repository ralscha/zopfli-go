package zopfli

import "hash/crc32"

func gzipCompress(options *Options, in []byte) []byte {
	writer := newBitWriter(estimateOutputCap(len(in)) + 18)
	writer.out = append(writer.out,
		31, 139, 8, 0,
		0, 0, 0, 0,
		2, 3,
	)
	deflate(options, 2, true, in, &writer)
	crcValue := crc32.ChecksumIEEE(in)
	writer.out = append(writer.out,
		byte(crcValue),
		byte(crcValue>>8),
		byte(crcValue>>16),
		byte(crcValue>>24),
		byte(len(in)),
		byte(len(in)>>8),
		byte(len(in)>>16),
		byte(len(in)>>24),
	)
	debugf(options, "Original Size: %d, Gzip: %d, Compression: %f%% Removed\n", len(in), len(writer.out), 100.0*float64(len(in)-len(writer.out))/float64(maxInt(len(in), 1)))
	return writer.bytes()
}
