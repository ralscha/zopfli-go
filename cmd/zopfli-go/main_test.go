package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
