package zopfli

import "hash/adler32"

func zlibCompress(options *Options, in []byte) []byte {
	writer := newBitWriter(estimateOutputCap(len(in)) + 8)
	checksum := adler32.Checksum(in)
	cmf := 120
	flevel := 3
	fdict := 0
	cmfFlg := 256*cmf + fdict*32 + flevel*64
	fcheck := 31 - cmfFlg%31
	cmfFlg += fcheck
	writer.out = append(writer.out, lowByteFromInt(cmfFlg/256), lowByteFromInt(cmfFlg))
	deflate(options, 2, true, in, &writer)
	writer.out = append(writer.out, lowByteFromUint32(checksum>>24), lowByteFromUint32(checksum>>16), lowByteFromUint32(checksum>>8), lowByteFromUint32(checksum))
	debugf(options, "Original Size: %d, Zlib: %d, Compression: %f%% Removed\n", len(in), len(writer.out), 100.0*float64(len(in)-len(writer.out))/float64(maxInt(len(in), 1)))
	return writer.bytes()
}
