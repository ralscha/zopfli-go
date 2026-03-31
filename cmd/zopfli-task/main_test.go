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
	if err := os.WriteFile(readmePath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write README fixture: %v", err)
	}

	summary := "some log output\nmore detail\n\n| Corpus | GoMs |\n| --- | ---: |\n| sample | 1.23 |\n\ntrailing notes\n"
	if err := syncReadmeBenchmarkSection(readmePath, summary); err != nil {
		t.Fatalf("sync README benchmark section: %v", err)
	}

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
