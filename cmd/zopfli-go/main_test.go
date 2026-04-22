package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
			if got := cfg.options.BlockSplittingLast; got != (tc.wantMode == blockSplittingLastModeTrue) {
				t.Fatalf("options.BlockSplittingLast = %t, want %t", got, tc.wantMode == blockSplittingLastModeTrue)
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
	compressed, err := os.ReadFile(gzipPath)
	if err != nil {
		t.Fatalf("read gzip output: %v", err)
	}
	decoded := gunzipBytes(t, compressed)
	if !bytes.Equal(decoded, sourceData) {
		t.Fatal("gzip output did not round-trip")
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
