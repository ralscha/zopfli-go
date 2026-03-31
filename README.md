# zopfli-go

`zopfli-go` is a pure Go implementation of Zopfli-style compression for `gzip`, `zlib`, and raw `deflate` output.

## Usage

```go
package main

import zopfli "github.com/ralscha/zopfli-go"

func main() {
	compressed := zopfli.Gzip([]byte("hello, world"))
}
```

For custom tuning, use `DefaultOptions()` and call `Compress` with `FormatGzip`, `FormatZlib`, or `FormatDeflate`.

```go
package main

import zopfli "github.com/ralscha/zopfli-go"

func main() {
	options := zopfli.DefaultOptions()
	options.NumIterations = 5
	options.BlockSplittingMax = 8

	compressed := zopfli.Compress(&options, zopfli.FormatGzip, []byte("hello, tuned world"))
}
```

## Benchmarks

The table below is updated by the benchmark workflow on branch pushes and workflow dispatches.

Benchmark comparisons use the original upstream Zopfli implementation from https://github.com/google/zopfli.

<!-- benchmark-summary:start -->

| Corpus | GoMs | PgoMs | CMs | PGO/Go | Go/C | PGO/C | GoBytes | CBytes | GzipBytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| mixed-256k | 2774.77 | 2847.28 | 7645.46 | 1.03 | 0.36 | 0.37 | 3162 | 3162 | 3204 |
| random-256k | 427.53 | 407.69 | 394.99 | 0.95 | 1.08 | 1.03 | 262183 | 262183 | 262247 |
| real-files-256k | 1010.16 | 1010.05 | 2601.00 | 1.00 | 0.39 | 0.39 | 4649 | 4649 | 5042 |
| records-logs-256k | 1118.81 | 1093.96 | 2234.74 | 0.98 | 0.50 | 0.49 | 2510 | 2510 | 2662 |
| tiny-text | 30.95 | 30.86 | 31.16 | 1.00 | 0.99 | 0.99 | 58 | 58 | 65 |
| web-assets-256k | 993.76 | 988.22 | 2312.59 | 0.99 | 0.43 | 0.43 | 3756 | 3756 | 4078 |

<!-- benchmark-summary:end -->


## Development

Run the package tests with:

```bash
go test ./...
```

Generate the benchmark summary locally with:

```bash
go run ./cmd/zopfli-task bench-summary
```
