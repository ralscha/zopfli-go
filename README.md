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
| mixed-256k | 3049.80 | 2992.09 | 6776.02 | 0.98 | 0.45 | 0.44 | 3162 | 3162 | 3204 |
| random-256k | 369.09 | 368.94 | 271.39 | 1.00 | 1.36 | 1.36 | 262183 | 262183 | 262247 |
| real-files-256k | 1090.49 | 1086.29 | 2313.43 | 1.00 | 0.47 | 0.47 | 4649 | 4649 | 5042 |
| records-logs-256k | 1192.50 | 1192.66 | 2091.21 | 1.00 | 0.57 | 0.57 | 2510 | 2510 | 2662 |
| tiny-text | 26.88 | 27.15 | 29.32 | 1.01 | 0.92 | 0.93 | 58 | 58 | 65 |
| web-assets-256k | 1083.51 | 1069.15 | 2119.01 | 0.99 | 0.51 | 0.50 | 3756 | 3756 | 4078 |

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

## Releases

GitHub releases are produced by GoReleaser from version tags such as `v1.0.0`.

Release assets are archived as `.tar.gz` on Linux and macOS, and as `.zip` on Windows.

Those release assets are consumed directly by `bread-compressor-cli` when its `--use-zopfli-go` flag is enabled.
