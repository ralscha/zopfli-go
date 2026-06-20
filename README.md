# zopfli-go

`zopfli-go` is a pure Go implementation of Zopfli-style compression for `gzip`, `zlib`, and raw `deflate` output.

## Go Usage

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

## CLI Usage

The repository includes a file-oriented CLI for precompressing web assets into adjacent `.gz` files.

```bash
./zopfli-go --help
./zopfli-go --jobs 8 public
./zopfli-go --include-suffix .js --exclude-suffix .min.js public
./zopfli-go public assets/app.js
./zopfli-go --json public
```

Behavior:

- File and directory inputs are accepted.
- Directories are walked recursively.
- Outputs are written next to the source file as `filename.ext.gz`.
- Files are skipped when the `.gz` output is larger than or equal to the original.
- When a file is skipped for size, any stale adjacent `.gz` output from an earlier run is removed.
- Existing `.gz` files are ignored as inputs unless `--allow-gzip-inputs` is set.

Supported CLI flags:

- `-j`, `--jobs`
- `-i`, `--include-suffix` and `-x`, `--exclude-suffix` (repeatable, matched against relative paths or base filenames)
- `--allow-gzip-inputs`
- `-n`, `--iterations`
- `--block-splitting`
- `--block-splitting-last=false|true|both`
- `--block-splitting-max`
- `-v`, `--verbose`
- `-V`, `--verbose-more`
- `-J`, `--json`

## Benchmarks

The table below is updated by the benchmark workflow on branch pushes and workflow dispatches.

Benchmark comparisons use the original upstream Zopfli implementation from https://github.com/google/zopfli.

<!-- benchmark-summary:start -->

| Corpus | GoMs | PgoMs | CMs | PGO/Go | Go/C | PGO/C | GoBytes | CBytes | GzipBytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| mixed-256k | 3171.17 | 2891.89 | 6801.61 | 0.91 | 0.47 | 0.43 | 3162 | 3162 | 3204 |
| random-256k | 410.50 | 394.40 | 376.88 | 0.96 | 1.09 | 1.05 | 262183 | 262183 | 262247 |
| real-files-256k | 1169.00 | 1060.99 | 2335.36 | 0.91 | 0.50 | 0.45 | 4649 | 4649 | 5042 |
| records-logs-256k | 1275.96 | 1150.95 | 2087.19 | 0.90 | 0.61 | 0.55 | 2510 | 2510 | 2662 |
| tiny-text | 28.59 | 26.69 | 29.35 | 0.93 | 0.97 | 0.91 | 58 | 58 | 65 |
| web-assets-256k | 1154.57 | 1042.36 | 2142.33 | 0.90 | 0.54 | 0.49 | 3756 | 3756 | 4078 |

<!-- benchmark-summary:end -->


## Development

Use the Go version declared in `go.mod`.

Run the package tests with:

```bash
go test ./...
```

Generate the benchmark summary locally with:

```bash
go run ./cmd/zopfli-task bench-summary
```

## Releases

GitHub releases are produced by GoReleaser from version tags such as `v1.0.0`.

Release assets are archived as `.tar.gz` on Linux and macOS, and as `.zip` on Windows.

Those release assets are consumed directly by `bread-compressor-cli` when its `--use-zopfli-go` flag is enabled.
