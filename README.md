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

	compressed := zopfli.CompressParallel(&options, zopfli.FormatGzip, []byte("hello, tuned world"), 4)
}
```

### Fast profile

`FastOptions()` starts with `DefaultOptions()` and changes exactly one setting:
`NumIterations` is reduced from `15` to `3`.

| Setting | Default | Fast |
| --- | ---: | ---: |
| Optimal parsing iterations | 15 | 3 |
| Block splitting | enabled | enabled |
| Maximum split points | 15 | 15 |

Fewer parsing iterations reduce CPU time, but can produce a slightly larger
compressed file because fewer candidate parses are evaluated. The output is
still a normal, deterministic gzip, zlib, or deflate stream. Fast mode does not
enable parallelism; use `CompressParallel` or CLI `--workers-per-file`
separately when compressing a large input.

```go
options := zopfli.FastOptions()
compressed := zopfli.Compress(&options, zopfli.FormatGzip, data)
```

The CLI `--fast` flag applies the same three-iteration profile. Explicit
`--iterations`, `--block-splitting`, and `--block-splitting-max` flags take
precedence over the profile.

`Compress` is serial. `CompressParallel` bounds parallel analysis inside one
file; keep its worker count at `1` when compressing many files concurrently, or
increase it for a small number of large inputs. Worker counts are capped at
`MaxCompressionWorkers` (currently `4`). For each active 1 MiB input block, the
match cache uses about 12 MiB of metadata plus 4 bytes per cached distance run,
with a 60 MiB hard ceiling; parsing and token buffers require additional memory.
The CLI divides its default `--jobs` value by `--workers-per-file` so the two
levels of concurrency do not multiply by default; an explicit `--jobs` value
overrides that safeguard.

## CLI Usage

The repository includes a file-oriented CLI for precompressing web assets into adjacent `.gz` files.

```bash
./zopfli-go --help
./zopfli-go --jobs 8 public
./zopfli-go --fast public
./zopfli-go --fast --jobs 1 --workers-per-file 4 large-asset.js
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
- `--fast`
- `-i`, `--include-suffix` and `-x`, `--exclude-suffix` (repeatable, matched against relative paths or base filenames)
- `--allow-gzip-inputs`
- `-n`, `--iterations`
- `--workers-per-file`
- `--block-splitting`
- `--block-splitting-last=false|true|both` (deprecated compatibility option)
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
