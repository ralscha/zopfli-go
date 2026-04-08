package zopfli

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	crand "crypto/rand"
	"io"
	"strings"
	"testing"
)

func getRandomBytes(length uint64) []byte {
	data := make([]byte, length)
	if _, err := crand.Read(data); err != nil {
		panic(err)
	}
	return data
}

func TestGzip(t *testing.T) {
	compressibleString := "compressthis" + strings.Repeat("_foobar", 1000) + "$"
	for _, test := range []struct {
		name    string
		data    []byte
		maxSize int
	}{
		{"compressible string", []byte(compressibleString), 500},
		{"random binary data", getRandomBytes(3000), 3100},
		{"empty string", []byte(""), 25},
	} {
		compressed := Gzip(test.data)
		decompressed := mustGunzipStdlib(t, test.name, compressed)
		if !bytes.Equal(test.data, decompressed) {
			t.Fatalf("%s: decompressed mismatch", test.name)
		}
		if test.maxSize > 0 && len(compressed) > test.maxSize {
			t.Fatalf("%s: compressed data is %d bytes, expected %d or less", test.name, len(compressed), test.maxSize)
		}
	}
}

func TestZlib(t *testing.T) {
	data := []byte(strings.Repeat("abcdefgabcdefgabcdefg", 200))
	compressed := Zlib(data)
	decompressed := mustZlibDecompressStdlib(t, "zlib", compressed)
	if !bytes.Equal(data, decompressed) {
		t.Fatal("zlib decompressed mismatch")
	}
}

func TestDeflate(t *testing.T) {
	data := []byte(strings.Repeat("zzzzzzzzzzzzzzzzzzzz", 128))
	compressed := Deflate(data)
	assertDeflateRoundTrip(t, "deflate", compressed, data)
}

func TestBenchmarkCorporaRoundTrip(t *testing.T) {
	t.Parallel()

	opt := DefaultOptions()
	for _, corpus := range benchmarkCorpora() {
		for _, format := range []struct {
			name   string
			format Format
			verify func(testing.TB, string, []byte, []byte)
		}{
			{name: "gzip", format: FormatGzip, verify: assertGzipRoundTrip},
			{name: "zlib", format: FormatZlib, verify: assertZlibRoundTrip},
			{name: "deflate", format: FormatDeflate, verify: assertDeflateRoundTrip},
		} {
			t.Run(corpus.name+"/"+format.name, func(t *testing.T) {
				compressed := Compress(&opt, format.format, corpus.data)
				format.verify(t, corpus.name+"/"+format.name, compressed, corpus.data)
			})
		}
	}
}

func assertGzipRoundTrip(tb testing.TB, testName string, compressed, want []byte) {
	tb.Helper()

	decompressed := mustGunzipStdlib(tb, testName, compressed)
	if !bytes.Equal(want, decompressed) {
		tb.Fatalf("%s: gzip decompressed mismatch", testName)
	}
}

func assertZlibRoundTrip(tb testing.TB, testName string, compressed, want []byte) {
	tb.Helper()

	decompressed := mustZlibDecompressStdlib(tb, testName, compressed)
	if !bytes.Equal(want, decompressed) {
		tb.Fatalf("%s: zlib decompressed mismatch", testName)
	}
}

func assertDeflateRoundTrip(tb testing.TB, testName string, compressed, want []byte) {
	tb.Helper()

	decompressed := mustFlateDecompressStdlib(tb, testName, compressed)
	if !bytes.Equal(want, decompressed) {
		tb.Fatalf("%s: deflate decompressed mismatch", testName)
	}
}

func mustGunzipStdlib(tb testing.TB, testName string, compressed []byte) []byte {
	tb.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		tb.Fatalf("%s: gzip.NewReader: %v", testName, err)
	}

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		tb.Fatalf("%s: read gzip: %v", testName, err)
	}
	if err := reader.Close(); err != nil {
		tb.Fatalf("%s: close gzip: %v", testName, err)
	}

	return decompressed
}

func mustZlibDecompressStdlib(tb testing.TB, testName string, compressed []byte) []byte {
	tb.Helper()

	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		tb.Fatalf("%s: zlib.NewReader: %v", testName, err)
	}

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		tb.Fatalf("%s: read zlib: %v", testName, err)
	}
	if err := reader.Close(); err != nil {
		tb.Fatalf("%s: close zlib: %v", testName, err)
	}

	return decompressed
}

func mustFlateDecompressStdlib(tb testing.TB, testName string, compressed []byte) []byte {
	tb.Helper()

	reader := flate.NewReader(bytes.NewReader(compressed))
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		tb.Fatalf("%s: read deflate: %v", testName, err)
	}
	if err := reader.Close(); err != nil {
		tb.Fatalf("%s: close deflate: %v", testName, err)
	}

	return decompressed
}
