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
AVX2 is not required. On platforms that support Go's CPU feature overrides,
test the runtime fallback with
`GOEXPERIMENT=simd GODEBUG=cpu.avx=off go test -count=1 ./...`.
The Windows Go 1.27.3 runtime used for these measurements ignores these CPU
overrides; a passing test with that setting does not establish fallback coverage.

The scan preserves the first strictly greater cost, including ties and NaNs,
and never reads outside the requested range. Compression settings and output
are unchanged. The implementation explicitly clears the upper AVX registers
before returning to scalar code, as required by the experimental API to avoid
transition penalties on older CPUs.

Profiling `main` on the benchmark corpora attributed about 44% of CPU samples to
this scan, 18% to updating match costs, and 3% to byte matching. Vectorizing the
cost updates was also tried but was slower on the i7-3610QM, so those updates
remain scalar in production. New AVX2 prototypes are measured below.
Hash-chain traversal and Huffman construction have dependencies
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

The compressible corpora take **14–27% less time** on the i7-3610QM; random data
shows no meaningful improvement.

Repeated on 2026-10-24 on an **Intel i7-11800H (AVX and AVX2)** with the same
Go version, baseline, corpora, and sampling method. The candidate is `4d8d4ec`.
Go's runtime feature checks confirm both AVX and AVX2 are available. Median
milliseconds and the ratio of the older CPU's SIMD time to this CPU's SIMD time:

| Corpus | Main ms | Main + experiment ms | SIMD ms | Time saved vs main | SIMD speedup vs i7-3610QM |
| --- | ---: | ---: | ---: | ---: | ---: |
| tiny-text | 10.66 | 9.96 | 8.30 | 22.1% | 2.03x |
| mixed-256k | 1802.39 | 2936.84 | 1518.12 | 15.8% | 1.79x |
| web-assets-256k | 627.06 | 668.45 | 428.66 | 31.6% | 1.88x |
| records-logs-256k | 708.02 | 744.20 | 501.32 | 29.2% | 1.92x |
| real-files-256k | 624.64 | 666.89 | 439.39 | 29.7% | 1.88x |
| random-256k | 121.44 | 121.59 | 120.63 | 0.7% | 2.00x |

On the i7-11800H, the compressible corpora take **16–32% less time**; the random
data difference is within the observed sample variation. Both CPUs execute the
same AVX implementation in these tables; the AVX2 broadcast experiment below
measures an additional change.
The unchanged `main` control with the experiment enabled is 62.9% slower on
`mixed-256k` on this machine, consistently across all six samples. Its cause
has not been isolated; the reported savings use ordinary `main` as the baseline.

Allocated bytes and allocation counts match between builds. SHA-256 comparisons
against `main` match for all six corpora in both default and fast modes, and the
i7-11800H output digests also match the saved i7-3610QM digests.

Raw measurements and methodology for the
[i7-3610QM](testdata/bench-results/simd/metadata.txt) and
[i7-11800H](testdata/bench-results/simd/i7-11800h/metadata.txt)
include the control build of unchanged `main` with the experiment enabled.
The same benchmark harness is used for all builds, including seeded random
input. The file fixtures use CRLF line endings; changing checkout line endings
changes the inputs. Despite its existing name, `mixed-256k` contains 812,032 bytes.

To repeat the comparison, copy the current `benchmark_test.go` into a separate
checkout of the baseline commit and build its test binary with
`GOEXPERIMENT` unset and `go test -c -pgo=off`. Build the candidate with
`GOEXPERIMENT=simd` and the same
command. Run both binaries from the same repository directory so they read
identical fixtures, alternating their order:

```text
<binary> -test.run=^$ -test.bench=^BenchmarkPureGoGzip$ -test.benchtime=2x -test.count=1 -test.cpu=1
```

An **AVX2 broadcast prototype** was also tested on the i7-11800H. Go's
[`archsimd` package supports AVX2](https://go.dev/blog/archsimd), including
`BroadcastFloat64x4` and `X86.AVX2()` feature detection. The prototype uses a
register broadcast to initialize the threshold on AVX2 CPUs, retaining the
existing AVX load on older CPUs. Both paths still compare four `float64` values.
Disassembly confirms that the AVX2 path emits `VBROADCASTSD`.

The initial six samples of two operations were noisy, so a confirmation run
used six samples of **five operations** per build, alternating AVX/AVX2 order.
These are fresh, paired build comparisons with the same compiler settings,
one Go CPU and PGO disabled; times are median milliseconds:

| Corpus | Current AVX ms | AVX2 prototype ms | Time saved vs AVX |
| --- | ---: | ---: | ---: |
| tiny-text | 8.20 | 7.85 | 4.3% |
| mixed-256k | 1478.97 | 1428.98 | 3.4% |
| web-assets-256k | 432.98 | 429.54 | 0.8% |
| records-logs-256k | 507.13 | 497.02 | 2.0% |
| real-files-256k | 446.63 | 434.79 | 2.7% |
| random-256k | 123.69 | 121.48 | 1.8% |

The additional compression benefit is modest. Every AVX2 `mixed-256k` sample
was faster than every AVX sample; ranges overlap for the other corpora, and
the random-data difference is within the observed variation. Early-hit scans
benefit more: `length-256/hit-0` falls from 9.132 ns to 2.869 ns (3.18x faster),
while the 256-entry no-hit scan stays near 51 ns. Output digests and allocations
match. Full suites pass for the prototype and for temporary builds that force
its AVX and scalar fallbacks; Windows CPU override limitations are noted above.

The prototype is saved as a
[reproducible patch](testdata/bench-results/simd/i7-11800h/avx2-broadcast/candidate.patch);
the production implementation remains unchanged. Both timing runs, assembly,
validation and methodology are in the
[AVX2 experiment notes](testdata/bench-results/simd/i7-11800h/avx2-broadcast/metadata.txt).

Three follow-up **AVX2 cost-update prototypes** target `relaxStatLengths`, which
accounts for 22.0% of samples in a fresh profile of the broadcast baseline.
They compare four costs at a time with individual winning-length writes,
pack eight length writes into a vector, or refine the first approach by keeping
its short-range scalar loop inside the same function.

The packed-write version improved long synthetic ranges but showed no convincing
compression benefit. The other two were confirmed over six samples of five
operations, with all six build-order permutations. These median milliseconds
compare against a fresh **AVX2 broadcast baseline**, isolating the cost updates:

| Corpus | Broadcast baseline ms | Scalar writes ms | Time saved | Inline scalar ms | Time saved |
| --- | ---: | ---: | ---: | ---: | ---: |
| tiny-text | 7.91 | 8.02 | -1.4% | 8.02 | -1.4% |
| mixed-256k | 1454.03 | 1367.48 | 6.0% | 1358.34 | 6.6% |
| web-assets-256k | 436.55 | 433.24 | 0.8% | 431.85 | 1.1% |
| records-logs-256k | 503.84 | 496.37 | 1.5% | 499.52 | 0.9% |
| real-files-256k | 440.19 | 437.41 | 0.6% | 437.37 | 0.6% |
| random-256k | 122.11 | 123.54 | -1.2% | 122.04 | 0.1% |

The simpler scalar-write candidate saves **6.2% and 6.0% on the mixed corpus**
in the initial and confirmation runs; every candidate sample beats every
baseline sample in both runs. Other corpus ranges overlap. The inline refinement
does not clearly outperform it or remove the tiny-text regression, so the simpler
[scalar-write patch](testdata/bench-results/simd/i7-11800h/avx2-relax/scalar-writes.patch)
is the strongest follow-up candidate. These incremental savings should not be
combined with earlier measurements from separate runs.

All 18 output hashes and allocation counts match. Full suites pass for all three
prototypes with SIMD enabled, disabled, and with the new relaxation fallback
forced. The production implementation remains unchanged. The
[cost-update experiment notes](testdata/bench-results/simd/i7-11800h/avx2-relax/metadata.txt)
retain all three patches, raw timings, assembly, profile and validation results.
The i7-3610QM would use the scalar fallback; these prototypes have not been
measured on that computer.

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
