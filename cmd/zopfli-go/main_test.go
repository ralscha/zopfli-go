package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	zopfli "github.com/ralscha/zopfli-go"
)

func TestRunSingleFileWritesAdjacentGzip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "app.js")
	sourceData := []byte(strings.Repeat("const value = 'compress-me';\n", 256))
	if err := os.WriteFile(sourcePath, sourceData, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{sourcePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}

	gzipPath := sourcePath + gzipExtension
	//nolint:gosec // Test reads a file it created in a temporary directory.
	compressed, err := os.ReadFile(gzipPath)
	if err != nil {
		t.Fatalf("read gzip output: %v", err)
	}
	decoded := gunzipBytes(t, compressed)
	if !bytes.Equal(decoded, sourceData) {
		t.Fatal("gzip output did not round-trip")
	}
	if !strings.Contains(stdout.String(), "Summary: written=1 skipped-bigger=0 skipped-filtered=0 errors=0") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRunDirectoryUsesFiltersAndSkipsLargerFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "js", "app.js"), []byte(strings.Repeat("console.log('app');\n", 256)))
	mustWriteFile(t, filepath.Join(root, "js", "ignore.js"), []byte(strings.Repeat("console.log('ignore');\n", 256)))
	mustWriteFile(t, filepath.Join(root, "css", "site.css"), []byte(strings.Repeat("body { color: red; }\n", 128)))
	mustWriteFile(t, filepath.Join(root, "js", "random.js"), randomBytes(4096))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--jobs", "2", "--include-suffix", ".js", "--exclude-suffix", "ignore.js", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}

	if _, err := os.Stat(filepath.Join(root, "js", "app.js.gz")); err != nil {
		t.Fatalf("expected app.js.gz to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "js", "ignore.js.gz")); !os.IsNotExist(err) {
		t.Fatalf("expected ignore.js.gz to be absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "css", "site.css.gz")); !os.IsNotExist(err) {
		t.Fatalf("expected site.css.gz to be absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "js", "random.js.gz")); !os.IsNotExist(err) {
		t.Fatalf("expected random.js.gz to be absent, got err=%v", err)
	}

	wantSummary := "Summary: written=1 skipped-bigger=1 skipped-filtered=2 errors=0"
	if !strings.Contains(stdout.String(), wantSummary) {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunJSONOutputUsesStructuredReport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "assets", "main.css"), []byte(strings.Repeat("body{color:#123456;}\n", 128)))
	mustWriteFile(t, filepath.Join(root, "assets", "data.bin"), randomBytes(2048))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"-J", "-i", ".css", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}

	var report struct {
		Summary struct {
			Written         int `json:"written"`
			SkippedBigger   int `json:"skippedBigger"`
			SkippedFiltered int `json:"skippedFiltered"`
			Errors          int `json:"errors"`
		} `json:"summary"`
		Results []struct {
			SourcePath string `json:"sourcePath"`
			Status     string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%s", err, stdout.String())
	}
	if report.Summary.Written != 1 || report.Summary.SkippedFiltered != 1 || report.Summary.Errors != 0 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if len(report.Results) != 2 {
		t.Fatalf("unexpected result count: %d", len(report.Results))
	}
}

func TestParseArgsSupportsBlockSplittingLastModes(t *testing.T) {
	t.Parallel()

	modeCases := []struct {
		name     string
		args     []string
		wantMode string
		wantArgs []string
	}{
		{
			name:     "default false",
			args:     []string{"asset.js"},
			wantMode: blockSplittingLastModeFalse,
			wantArgs: []string{"asset.js"},
		},
		{
			name:     "bool flag defaults to true",
			args:     []string{"--block-splitting-last", "asset.js"},
			wantMode: blockSplittingLastModeTrue,
			wantArgs: []string{"asset.js"},
		},
		{
			name:     "explicit both value",
			args:     []string{"--block-splitting-last=both", "asset.js"},
			wantMode: blockSplittingLastModeBoth,
			wantArgs: []string{"asset.js"},
		},
	}

	for _, tc := range modeCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, inputs, showHelp, err := parseArgs(tc.args)
			if err != nil {
				t.Fatalf("parseArgs returned error: %v", err)
			}
			if showHelp {
				t.Fatal("parseArgs unexpectedly requested help")
			}
			if cfg.blockSplittingLastMode != tc.wantMode {
				t.Fatalf("blockSplittingLastMode = %q, want %q", cfg.blockSplittingLastMode, tc.wantMode)
			}
			if !slices.Equal(inputs, tc.wantArgs) {
				t.Fatalf("inputs = %v, want %v", inputs, tc.wantArgs)
			}
		})
	}

	_, _, _, err := parseArgs([]string{"--block-splitting-last=maybe", "asset.js"})
	if err == nil || !strings.Contains(err.Error(), "false, true, or both") {
		t.Fatalf("expected validation error for invalid block-splitting-last value, got %v", err)
	}
}

func TestParseArgsFastProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "fast default", args: []string{"--fast", "asset.js"}, want: 3},
		{name: "long override before", args: []string{"--iterations=7", "--fast", "asset.js"}, want: 7},
		{name: "short override after", args: []string{"--fast", "-n", "5", "asset.js"}, want: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, _, err := parseArgs(tc.args)
			if err != nil {
				t.Fatalf("parseArgs returned error: %v", err)
			}
			if cfg.options.NumIterations != tc.want {
				t.Fatalf("NumIterations = %d, want %d", cfg.options.NumIterations, tc.want)
			}
		})
	}
	cfg, _, _, err := parseArgs([]string{"--fast", "--workers-per-file=4", "asset.js"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.workersPerFile != 4 {
		t.Fatalf("explicit fast-profile workers = %d, want 4", cfg.workersPerFile)
	}
}

func TestParseArgsRejectsNonPositiveIterations(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "-1"} {
		if _, _, _, err := parseArgs([]string{"--iterations=" + value, "asset.js"}); err == nil {
			t.Fatalf("expected validation error for %s iterations", value)
		}
	}
}

func TestParseArgsWorkersPerFile(t *testing.T) {
	t.Parallel()

	cfg, _, _, err := parseArgs([]string{"--workers-per-file=4", "asset.js"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.workersPerFile != 4 {
		t.Fatalf("workersPerFile = %d, want 4", cfg.workersPerFile)
	}
	wantDefaultJobs := max(1, runtime.GOMAXPROCS(0)/4)
	if cfg.jobs != wantDefaultJobs {
		t.Fatalf("default jobs = %d, want %d with four workers per file", cfg.jobs, wantDefaultJobs)
	}
	explicit, _, _, err := parseArgs([]string{"--jobs=7", "--workers-per-file=4", "asset.js"})
	if err != nil {
		t.Fatalf("parse explicit jobs and workers: %v", err)
	}
	if explicit.jobs != 7 {
		t.Fatalf("explicit jobs = %d, want 7", explicit.jobs)
	}
	if _, _, _, err := parseArgs([]string{"--workers-per-file=0", "asset.js"}); err == nil {
		t.Fatal("expected validation error for zero workers")
	}
	if _, _, _, err := parseArgs([]string{"--workers-per-file=5", "asset.js"}); err == nil {
		t.Fatal("expected validation error above the worker cap")
	}
}

func TestRunSupportsBlockSplittingLastBoth(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "app.js")
	sourceData := []byte(strings.Repeat("const important = 'compress-me';\n", 256))
	if err := os.WriteFile(sourcePath, sourceData, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--block-splitting-last=both", sourcePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}

	gzipPath := sourcePath + gzipExtension
	//nolint:gosec // gzipPath is derived from a path created under t.TempDir.
	compressed, err := os.ReadFile(gzipPath)
	if err != nil {
		t.Fatalf("read gzip output: %v", err)
	}
	decoded := gunzipBytes(t, compressed)
	if !bytes.Equal(decoded, sourceData) {
		t.Fatal("gzip output did not round-trip")
	}
}

func TestBlockSplittingLastBothCompressesOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	got := compressCandidateWith(zopfli.DefaultOptions(), blockSplittingLastModeBoth, []byte("data"), 4, func(_ *zopfli.Options, format zopfli.Format, data []byte, numWorkers int) []byte {
		calls++
		if format != zopfli.FormatGzip {
			t.Fatalf("format = %v, want gzip", format)
		}
		if numWorkers != 4 {
			t.Fatalf("numWorkers = %d, want 4", numWorkers)
		}
		return append([]byte(nil), data...)
	})
	if calls != 1 {
		t.Fatalf("compression calls = %d, want 1", calls)
	}
	if !bytes.Equal(got, []byte("data")) {
		t.Fatalf("compressed data = %q, want data", got)
	}
}

func TestRunAllowGzipInputsWritesDoubleGzip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "app.js.gz")
	sourceData := []byte(strings.Repeat("const important = 'compress-me';\n", 256))
	if err := os.WriteFile(sourcePath, sourceData, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--allow-gzip-inputs", sourcePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}

	gzipPath := sourcePath + gzipExtension
	//nolint:gosec // gzipPath is derived from a path created under t.TempDir.
	compressed, err := os.ReadFile(gzipPath)
	if err != nil {
		t.Fatalf("read gzip output: %v", err)
	}
	decoded := gunzipBytes(t, compressed)
	if !bytes.Equal(decoded, sourceData) {
		t.Fatal("gzip output did not round-trip")
	}
}

func gunzipBytes(t *testing.T, compressed []byte) []byte {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			t.Fatalf("close gzip reader: %v", closeErr)
		}
	}()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	return decoded
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func randomBytes(length int) []byte {
	data := make([]byte, length)
	if _, err := crand.Read(data); err != nil {
		panic(err)
	}
	return data
}
