package zopfli

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"math/rand"
	"strings"
	"testing"
)

func getRandomBytes(length uint64) []byte {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, length)
	for i := range length {
		data[i] = byte(rng.Int())
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
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatalf("%s: gzip.NewReader: %v", test.name, err)
		}
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("%s: read gzip: %v", test.name, err)
		}
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
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, decompressed) {
		t.Fatal("zlib decompressed mismatch")
	}
}

func TestDeflate(t *testing.T) {
	data := []byte(strings.Repeat("zzzzzzzzzzzzzzzzzzzz", 128))
	compressed := Deflate(data)
	reader := flate.NewReader(bytes.NewReader(compressed))
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, decompressed) {
		t.Fatal("deflate decompressed mismatch")
	}
}
