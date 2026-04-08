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
go run ./cmd/zopfli-go --help
go run ./cmd/zopfli-go --jobs 8 public
go run ./cmd/zopfli-go --include-suffix .js --exclude-suffix .min.js public
go run ./cmd/zopfli-go public assets/app.js
go run ./cmd/zopfli-go --json public
```

Behavior:

- File and directory inputs are accepted.
- Directories are walked recursively.
- Outputs are written next to the source file as `filename.ext.gz`.
- Files are skipped when the `.gz` output is larger than or equal to the original.
- Existing `.gz` files are ignored as inputs.

Supported CLI flags:

- `-j`, `--jobs`
- `-i`, `--include-suffix` and `-x`, `--exclude-suffix` (repeatable, matched against relative paths or base filenames)
- `-n`, `--iterations`
- `--block-splitting`
- `--block-splitting-last`
- `--block-splitting-max`
- `-v`, `--verbose`
- `-V`, `--verbose-more`
- `-J`, `--json`

## NPM Wrapper

The npm package wraps the file-oriented CLI rather than exposing buffer-to-buffer compression helpers.

```js
const { precompress, precompressSync } = require('zopfli-go');

const report = await precompress(['public'], {
	jobs: 8,
	includeSuffixes: ['.js', '.css'],
	excludeSuffixes: ['.min.js'],
});

const syncReport = precompressSync(['public', 'assets/app.js'], {
	iterations: 20,
	verbose: true,
});

console.log(report.summary.written, syncReport.summary.skippedBigger);
```

The wrapper exports:

- `precompress(inputs, options)`
- `precompressSync(inputs, options)`
- `buildArgs(inputs, options)`
- `getBinaryPath()`

`inputs` can be a single path or an array of file and directory paths. The wrapper forces `--json`, returns the parsed report object, and throws when the binary exits non-zero.

## Benchmarks

The table below is updated by the benchmark workflow on branch pushes and workflow dispatches.

Benchmark comparisons use the original upstream Zopfli implementation from https://github.com/google/zopfli.

<!-- benchmark-summary:start -->

| Corpus | GoMs | PgoMs | CMs | PGO/Go | Go/C | PGO/C | GoBytes | CBytes | GzipBytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| mixed-256k | 3165.09 | 2890.55 | 6910.05 | 0.91 | 0.46 | 0.42 | 3162 | 3162 | 3204 |
| random-256k | 411.83 | 388.92 | 299.72 | 0.94 | 1.37 | 1.30 | 262183 | 262183 | 262247 |
| real-files-256k | 1165.14 | 1064.94 | 2341.34 | 0.91 | 0.50 | 0.45 | 4649 | 4649 | 5042 |
| records-logs-256k | 1274.22 | 1140.47 | 2109.17 | 0.90 | 0.60 | 0.54 | 2510 | 2510 | 2662 |
| tiny-text | 28.32 | 27.23 | 28.78 | 0.96 | 0.98 | 0.95 | 58 | 58 | 65 |
| web-assets-256k | 1158.52 | 1034.38 | 2137.57 | 0.89 | 0.54 | 0.48 | 3756 | 3756 | 4078 |

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

The npm package is the root package in this repository. Its implementation lives under `npm/`, and its `postinstall` script downloads the matching GitHub release binary for the current package version.

Publish the npm package separately from a local machine after the matching GitHub release exists:

```bash
task npm-publish
```
