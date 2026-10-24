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

## Experimental SIMD

Go 1.27's [SIMD experiment](https://go.dev/blog/simd-experiment) can accelerate
the optimal parser's search for the first improvable match cost:

```bash
GOEXPERIMENT=simd go build ./cmd/zopfli-go
GOEXPERIMENT=simd go test ./...
```

In PowerShell, set `$env:GOEXPERIMENT = 'simd'` before those Go commands.
Ordinary builds keep the scalar implementation. Experimental builds use
`simd/archsimd` to compare four `float64` costs at once on amd64 CPUs with AVX;
other architectures and CPUs without AVX use the scalar implementation.
AVX2 is not required. Test the runtime fallback with
`GOEXPERIMENT=simd GODEBUG=cpu.avx=off go test -count=1 ./...`.

The scan preserves the first strictly greater cost, including ties and NaNs,
and never reads outside the requested range. Compression settings and output
are unchanged. The implementation explicitly clears the upper AVX registers
before returning to scalar code, as required by the experimental API to avoid
transition penalties on older CPUs.

Profiling `main` on the benchmark corpora attributed about 44% of CPU samples to
this scan, 18% to updating match costs, and 3% to byte matching. Vectorizing the
cost updates was also tried but was slower on the measured CPU, so those updates
remain scalar. Hash-chain traversal and Huffman construction have dependencies
that make them less suitable for this SIMD change.

Measured on 2026-10-24 with Go 1.27.3, Windows/amd64, and an Intel i7-3610QM
(AVX, without AVX2), against `main` at `8dde94a`. These are median milliseconds
for default gzip compression over six samples of two operations each, with one
Go CPU and PGO disabled. Run order rotates between the three builds.

| Corpus | Main ms | Main + experiment ms | SIMD ms | Time saved vs main |
| --- | ---: | ---: | ---: | ---: |
| tiny-text | 19.58 | 19.82 | 16.89 | 13.8% |
| mixed-256k | 3226.92 | 3163.53 | 2713.80 | 15.9% |
| web-assets-256k | 1098.00 | 1108.57 | 806.48 | 26.5% |
| records-logs-256k | 1253.54 | 1252.42 | 962.17 | 23.2% |
| real-files-256k | 1109.60 | 1104.28 | 824.07 | 25.7% |
| random-256k | 240.29 | 240.22 | 241.71 | -0.6% |

The compressible corpora take **14–27% less time** on this machine; random data
shows no meaningful improvement. Allocated bytes and allocation counts match.
SHA-256 comparisons against `main` match for all six corpora in both default
and fast modes. Other CPUs and workloads may have different results.

[Raw measurements and methodology](testdata/bench-results/simd/metadata.txt)
include a control build of unchanged `main` with the experiment enabled.
The same benchmark harness is used for all builds, including seeded random
input. Despite its existing name, `mixed-256k` contains 812,032 bytes.

To repeat the comparison, copy the current `benchmark_test.go` into a separate
checkout of the baseline commit and build its test binary with
`GOEXPERIMENT` unset and `go test -c -pgo=off`. Build the candidate with
`GOEXPERIMENT=simd` and the same
command. Run both binaries from the same repository directory so they read
identical fixtures, alternating their order:

```text
<binary> -test.run=^$ -test.bench=^BenchmarkPureGoGzip$ -test.benchtime=2x -test.count=1 -test.cpu=1
```

## Benchmarks

The table below is updated by the benchmark workflow on branch pushes and workflow dispatches.

Benchmark comparisons use the original upstream Zopfli implementation from https://github.com/google/zopfli.

<!-- benchmark-summary:start -->

| Corpus | GoMs | PgoMs | CMs | PGO/Go | Go/C | PGO/C | GoBytes | CBytes | GzipBytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| mixed-256k | 2135.48 | 2108.78 | 7612.31 | 0.99 | 0.28 | 0.28 | 3162 | 3162 | 3201 |
| random-256k | 172.85 | 162.91 | 377.88 | 0.94 | 0.46 | 0.43 | 262183 | 262183 | 262204 |
| real-files-256k | 739.31 | 688.92 | 2577.39 | 0.93 | 0.29 | 0.27 | 4649 | 4649 | 5033 |
| records-logs-256k | 834.06 | 768.70 | 2238.52 | 0.92 | 0.37 | 0.34 | 2510 | 2510 | 2661 |
| tiny-text | 14.84 | 15.87 | 30.52 | 1.07 | 0.49 | 0.52 | 58 | 58 | 62 |
| web-assets-256k | 739.50 | 678.32 | 2327.62 | 0.92 | 0.32 | 0.29 | 3756 | 3756 | 4073 |

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
