package zopfli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

type benchmarkCorpus struct {
	name string
	data []byte
}

var (
	benchmarkCorpusOnce  sync.Once
	benchmarkCorpusCache []benchmarkCorpus
)

func benchmarkCorpora() []benchmarkCorpus {
	benchmarkCorpusOnce.Do(func() {
		textBlock := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 16)
		configBlock := strings.Repeat("name=alpha\nmode=release\ncache=true\nworkers=8\n", 64)
		htmlBlock := strings.Repeat("<div class=\"row\"><span>compress me</span><span>1234567890</span></div>\n", 128)
		mixed := strings.Repeat(textBlock+configBlock+htmlBlock, 64)

		files := loadBenchmarkFiles()
		webMix := repeatToSize(files["landing.html"]+"\n"+files["dashboard.js"], 256*1024)
		serviceMix := repeatToSize(files["records.json"]+"\n"+files["service.log"], 256*1024)
		allFiles := make([]string, 0, len(files))
		for name := range files {
			allFiles = append(allFiles, name)
		}
		sort.Strings(allFiles)
		var sourceMixBuilder strings.Builder
		for _, name := range allFiles {
			sourceMixBuilder.WriteString("-- ")
			sourceMixBuilder.WriteString(name)
			sourceMixBuilder.WriteString(" --\n")
			sourceMixBuilder.WriteString(files[name])
			sourceMixBuilder.WriteString("\n")
		}

		benchmarkCorpusCache = []benchmarkCorpus{
			{name: "tiny-text", data: []byte(strings.Repeat("hello hello hello zopfli ", 128))},
			{name: "mixed-256k", data: []byte(mixed)},
			{name: "web-assets-256k", data: []byte(webMix)},
			{name: "records-logs-256k", data: []byte(serviceMix)},
			{name: "real-files-256k", data: []byte(repeatToSize(sourceMixBuilder.String(), 256*1024))},
			{name: "random-256k", data: getRandomBytes(256 * 1024)},
		}
	})
	return benchmarkCorpusCache
}

func loadBenchmarkFiles() map[string]string {
	dir := filepath.Join("testdata", "bench")
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(fmt.Sprintf("read benchmark corpus dir: %v", err))
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		//nolint:gosec // Benchmarks read repository-owned fixture files.
		data, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("read benchmark corpus file %s: %v", path, err))
		}
		result[entry.Name()] = string(data)
	}
	return result
}

func repeatToSize(value string, target int) string {
	if len(value) >= target {
		return value[:target]
	}
	var builder strings.Builder
	builder.Grow(target)
	for builder.Len() < target {
		remaining := target - builder.Len()
		if remaining >= len(value) {
			builder.WriteString(value)
		} else {
			builder.WriteString(value[:remaining])
		}
	}
	return builder.String()
}

func benchmarkOptions() *Options {
	opt := DefaultOptions()
	return &opt
}

func BenchmarkPureGoGzip(b *testing.B) {
	for _, corpus := range benchmarkCorpora() {
		b.Run(corpus.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(corpus.data)))
			opt := benchmarkOptions()
			var out []byte
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out = Compress(opt, FormatGzip, corpus.data)
			}
			b.StopTimer()
			assertGzipRoundTrip(b, corpus.name, out, corpus.data)
		})
	}
}

func BenchmarkPureGoDeflate(b *testing.B) {
	for _, corpus := range benchmarkCorpora() {
		b.Run(corpus.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(corpus.data)))
			opt := benchmarkOptions()
			var out []byte
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out = Compress(opt, FormatDeflate, corpus.data)
			}
			b.StopTimer()
			assertDeflateRoundTrip(b, corpus.name, out, corpus.data)
		})
	}
}

func BenchmarkUpstreamCGzip(b *testing.B) {
	exe := findUpstreamZopfliExe()
	if exe == "" {
		b.Skip("set ZOPFLI_UPSTREAM_EXE or ZOPFLI_UPSTREAM_ROOT to enable C comparison")
	}
	for _, corpus := range benchmarkCorpora() {
		b.Run(corpus.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(corpus.data)))
			opt := benchmarkOptions()
			iterationArg := fmt.Sprintf("--i%d", opt.NumIterations)
			inputFile := writeBenchmarkInputFile(b, corpus.name, corpus.data)
			var out []byte
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				//nolint:gosec // Benchmarks intentionally execute a user-configured local upstream binary.
				cmd := exec.Command(exe, "-c", "--gzip", iterationArg, inputFile)
				cmd.Stderr = nil
				var err error
				out, err = cmd.Output()
				if err != nil {
					b.Fatalf("upstream zopfli failed: %v", err)
				}
				if len(out) == 0 {
					b.Fatal("upstream zopfli produced empty output")
				}
			}
			b.StopTimer()
			assertGzipRoundTrip(b, corpus.name, out, corpus.data)
		})
	}
}

func BenchmarkCompressionRatio(b *testing.B) {
	for _, corpus := range benchmarkCorpora() {
		b.Run(corpus.name, func(b *testing.B) {
			opt := benchmarkOptions()
			exe := findUpstreamZopfliExe()
			iterationArg := fmt.Sprintf("--i%d", opt.NumIterations)
			inputFile := ""
			gzipOut := mustCompressStdlibGzip(b, corpus.data)
			if exe != "" {
				inputFile = writeBenchmarkInputFile(b, corpus.name, corpus.data)
			}
			var goOut []byte
			var cOut []byte
			b.ReportAllocs()
			b.SetBytes(int64(len(corpus.data)))
			for i := 0; i < b.N; i++ {
				goOut = Compress(opt, FormatGzip, corpus.data)
				if exe != "" {
					//nolint:gosec // Benchmarks intentionally execute a user-configured local upstream binary.
					cmd := exec.Command(exe, "-c", "--gzip", iterationArg, inputFile)
					var err error
					cOut, err = cmd.Output()
					if err != nil {
						b.Fatalf("upstream zopfli failed: %v", err)
					}
				}
			}
			b.StopTimer()
			assertGzipRoundTrip(b, corpus.name+"/go", goOut, corpus.data)
			b.ReportMetric(float64(len(goOut)), "go_bytes")
			b.ReportMetric(float64(len(gzipOut)), "gzip_bytes")
			b.ReportMetric(float64(len(corpus.data))/float64(len(goOut)), "go_ratio")
			if exe != "" {
				assertGzipRoundTrip(b, corpus.name+"/c", cOut, corpus.data)
				b.ReportMetric(float64(len(cOut)), "c_bytes")
				b.ReportMetric(float64(len(corpus.data))/float64(len(cOut)), "c_ratio")
			}
		})
	}
}

func mustCompressStdlibGzip(b *testing.B, data []byte) []byte {
	b.Helper()
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		b.Fatalf("create stdlib gzip writer: %v", err)
	}
	if _, err := writer.Write(data); err != nil {
		b.Fatalf("write stdlib gzip data: %v", err)
	}
	if err := writer.Close(); err != nil {
		b.Fatalf("close stdlib gzip writer: %v", err)
	}
	return buffer.Bytes()
}

func BenchmarkOptionProfiles(b *testing.B) {
	profiles := []struct {
		name string
		opt  Options
	}{
		{name: "default", opt: DefaultOptions()},
		{name: "iter3", opt: Options{NumIterations: 3, BlockSplitting: true, BlockSplittingLast: false, BlockSplittingMax: 15}},
		{name: "iter1", opt: Options{NumIterations: 1, BlockSplitting: true, BlockSplittingLast: false, BlockSplittingMax: 15}},
		{name: "iter3-nosplit", opt: Options{NumIterations: 3, BlockSplitting: false, BlockSplittingLast: false, BlockSplittingMax: 15}},
	}
	targets := map[string]struct{}{
		"web-assets-256k":   {},
		"records-logs-256k": {},
		"real-files-256k":   {},
	}
	for _, corpus := range benchmarkCorpora() {
		if _, ok := targets[corpus.name]; !ok {
			continue
		}
		for _, profile := range profiles {
			b.Run(corpus.name+"/"+profile.name, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(corpus.data)))
				opt := profile.opt
				var out []byte
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out = Compress(&opt, FormatGzip, corpus.data)
				}
				b.StopTimer()
				assertGzipRoundTrip(b, corpus.name+"/"+profile.name, out, corpus.data)
				b.ReportMetric(float64(len(out)), "bytes")
				b.ReportMetric(float64(len(corpus.data))/float64(len(out)), "ratio")
			})
		}
	}
}

func findUpstreamZopfliExe() string {
	if exe := os.Getenv("ZOPFLI_UPSTREAM_EXE"); exe != "" {
		//nolint:gosec // Benchmark helper checks a user-provided local executable path.
		if stat, err := os.Stat(exe); err == nil && !stat.IsDir() {
			return exe
		}
	}
	for _, root := range findUpstreamZopfliRoots() {
		for _, candidate := range upstreamExecutableCandidates(root) {
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

func findUpstreamZopfliRoots() []string {
	roots := make([]string, 0, 5)
	seen := map[string]struct{}{}
	add := func(path string) {
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}
		if stat, err := os.Stat(filepath.Join(cleaned, "src", "zopfli", "zopfli_bin.c")); err == nil && !stat.IsDir() {
			seen[cleaned] = struct{}{}
			roots = append(roots, cleaned)
		}
	}

	if root := os.Getenv("ZOPFLI_UPSTREAM_ROOT"); root != "" {
		add(root)
	}
	if exe := os.Getenv("ZOPFLI_UPSTREAM_EXE"); exe != "" {
		add(filepath.Dir(exe))
		add(filepath.Dir(filepath.Dir(exe)))
	}
	add(filepath.Join("..", "zopfli"))
	add(filepath.Join("..", "..", "zopfli"))
	add(filepath.Join("..", "upstream", "zopfli"))
	return roots
}

func upstreamExecutableCandidates(root string) []string {
	names := []string{"zopfli"}
	if exeExt := executableExtension(); exeExt != "" {
		names = append([]string{"zopfli" + exeExt}, names...)
	} else {
		names = append(names, "zopfli.exe")
	}

	candidates := make([]string, 0, len(names)*4)
	for _, name := range names {
		candidates = append(candidates,
			filepath.Join(root, "build-go-bench", name),
			filepath.Join(root, name),
			filepath.Join(root, "build", name),
			filepath.Join(root, "out", name),
		)
	}
	return candidates
}

func executableExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func writeBenchmarkInputFile(b *testing.B, name string, data []byte) string {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, name+".bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatalf("write input file: %v", err)
	}
	return path
}
