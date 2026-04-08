package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncReadmeBenchmarkSectionWritesTableOnly(t *testing.T) {
	t.Parallel()

	readmePath := filepath.Join(t.TempDir(), "README.md")
	initial := "# test\n\n" + readmeBenchStartMarker + "\nold content\n" + readmeBenchEndMarker + "\n"
	if err := os.WriteFile(readmePath, []byte(initial), 0o600); err != nil {
		t.Fatalf("write README fixture: %v", err)
	}

	summary := "some log output\nmore detail\n\n| Corpus | GoMs |\n| --- | ---: |\n| sample | 1.23 |\n\ntrailing notes\n"
	if err := syncReadmeBenchmarkSection(readmePath, summary); err != nil {
		t.Fatalf("sync README benchmark section: %v", err)
	}

	//nolint:gosec // Test reads a file it created in a temporary directory.
	gotBytes, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read updated README: %v", err)
	}

	table := "| Corpus | GoMs |\n| --- | ---: |\n| sample | 1.23 |"
	want := "# test\n\n" + readmeBenchStartMarker + "\n\n" + table + "\n\n" + readmeBenchEndMarker + "\n"
	if string(gotBytes) != want {
		t.Fatalf("unexpected README content:\nwant:\n%s\n\ngot:\n%s", want, string(gotBytes))
	}
	if string(gotBytes) == initial {
		t.Fatal("README benchmark section was not updated")
	}
}

func TestExtractMarkdownTable(t *testing.T) {
	t.Parallel()

	table, err := extractMarkdownTable("prefix\n| A | B |\n| --- | ---: |\n| x | 1 |\n\nsuffix")
	if err != nil {
		t.Fatalf("extract markdown table: %v", err)
	}

	want := "| A | B |\n| --- | ---: |\n| x | 1 |"
	if table != want {
		t.Fatalf("unexpected table:\nwant:\n%s\n\ngot:\n%s", want, table)
	}
}

func TestCompareBenchmarkFilesPassesWithinThreshold(t *testing.T) {
	t.Parallel()

	baselinePath := filepath.Join(t.TempDir(), "baseline.txt")
	candidatePath := filepath.Join(t.TempDir(), "candidate.txt")

	baseline := "BenchmarkPureGoGzip/web-assets-256k-8 1 1000000000 ns/op 0.26 MB/s\nBenchmarkPureGoGzip/records-logs-256k-8 1 2000000000 ns/op 0.13 MB/s\n"
	candidate := "BenchmarkPureGoGzip/web-assets-256k-8 1 1030000000 ns/op 0.25 MB/s\nBenchmarkPureGoGzip/records-logs-256k-8 1 1940000000 ns/op 0.14 MB/s\n"
	if err := os.WriteFile(baselinePath, []byte(baseline), 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	if err := os.WriteFile(candidatePath, []byte(candidate), 0o600); err != nil {
		t.Fatalf("write candidate: %v", err)
	}

	report, err := compareBenchmarkFiles(baselinePath, candidatePath, 5)
	if err != nil {
		t.Fatalf("compare benchmark files: %v", err)
	}
	if report.RegressionCount != 0 {
		t.Fatalf("unexpected regression count: %d", report.RegressionCount)
	}
	if report.MissingCount != 0 {
		t.Fatalf("unexpected missing count: %d", report.MissingCount)
	}
	if report.SharedBenchmarks != 2 {
		t.Fatalf("unexpected shared benchmark count: %d", report.SharedBenchmarks)
	}
	if report.NewBenchmarkCount != 0 {
		t.Fatalf("unexpected new benchmark count: %d", report.NewBenchmarkCount)
	}
	if report.Markdown == "" {
		t.Fatal("expected markdown output")
	}
}

func TestCompareBenchmarkFilesFailsOnRegressionAndMissingBenchmarks(t *testing.T) {
	t.Parallel()

	baselinePath := filepath.Join(t.TempDir(), "baseline.txt")
	candidatePath := filepath.Join(t.TempDir(), "candidate.txt")

	baseline := "BenchmarkPureGoGzip/web-assets-256k-8 1 1000000000 ns/op 0.26 MB/s\nBenchmarkPureGoGzip/records-logs-256k-8 1 2000000000 ns/op 0.13 MB/s\n"
	candidate := "BenchmarkPureGoGzip/web-assets-256k-8 1 1100000000 ns/op 0.24 MB/s\nBenchmarkPureGoGzip/new-corpus-256k-8 1 900000000 ns/op 0.29 MB/s\n"
	if err := os.WriteFile(baselinePath, []byte(baseline), 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	if err := os.WriteFile(candidatePath, []byte(candidate), 0o600); err != nil {
		t.Fatalf("write candidate: %v", err)
	}

	report, err := compareBenchmarkFiles(baselinePath, candidatePath, 5)
	if err == nil {
		t.Fatal("expected regression error")
	}
	if report.RegressionCount != 1 {
		t.Fatalf("unexpected regression count: %d", report.RegressionCount)
	}
	if report.MissingCount != 1 {
		t.Fatalf("unexpected missing count: %d", report.MissingCount)
	}
	if report.NewBenchmarkCount != 1 {
		t.Fatalf("unexpected new benchmark count: %d", report.NewBenchmarkCount)
	}
	if report.SharedBenchmarks != 1 {
		t.Fatalf("unexpected shared benchmark count: %d", report.SharedBenchmarks)
	}
	if report.Markdown == "" {
		t.Fatal("expected markdown output")
	}
}
