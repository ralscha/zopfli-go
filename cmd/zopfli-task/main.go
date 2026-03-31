package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultProfilePath       = "cpu-real.out"
	defaultBenchFilter       = "PureGoGzip/(web-assets-256k|records-logs-256k|real-files-256k)$"
	defaultBenchTime         = "1x"
	defaultBenchPackage      = "."
	defaultReadmePath        = "README.md"
	defaultUpstreamBench     = "PureGo|UpstreamC|CompressionRatio"
	defaultPureGoBench       = "PureGoGzip"
	defaultCompressionBench  = "CompressionRatio"
	repoModulePath           = "module github.com/ralscha/zopfli-go"
	upstreamRootEnv          = "ZOPFLI_UPSTREAM_ROOT"
	upstreamExeEnv           = "ZOPFLI_UPSTREAM_EXE"
	compilerEnv              = "CC"
	goCommand                = "go"
	upstreamBinaryBase       = "zopfli"
	upstreamSourceSentinel   = "src/zopfli/zopfli_bin.c"
	profileDescriptionPrefix = "PGO: "
	readmeBenchStartMarker   = "<!-- benchmark-summary:start -->"
	readmeBenchEndMarker     = "<!-- benchmark-summary:end -->"
)

var (
	benchPurePattern = regexp.MustCompile(`^BenchmarkPureGoGzip/(.+)-\d+\s+\d+\s+(\d+) ns/op\s+([0-9.]+) MB/s`)
	benchCPattern    = regexp.MustCompile(`^BenchmarkUpstreamCGzip/(.+)-\d+\s+\d+\s+(\d+) ns/op\s+([0-9.]+) MB/s`)
	benchRatioPrefix = regexp.MustCompile(`^BenchmarkCompressionRatio/(.+)-\d+\s+\d+\s+(\d+) ns/op\s+([0-9.]+) MB/s(.*)$`)
)

type optionalFloat struct {
	value float64
	ok    bool
}

type summaryRow struct {
	Corpus    string
	GoNs      optionalFloat
	PgoNs     optionalFloat
	CNs       optionalFloat
	GoBytes   optionalFloat
	CBytes    optionalFloat
	GzipBytes optionalFloat
	GoRatio   optionalFloat
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "build-upstream":
		err = runBuildUpstream(os.Args[2:])
	case "bench-upstream":
		err = runBenchUpstream(os.Args[2:])
	case "capture-cpu-profile":
		err = runCaptureCPUProfile(os.Args[2:])
	case "bench-pgo":
		err = runBenchPGO(os.Args[2:])
	case "bench-summary":
		err = runBenchSummary(os.Args[2:])
	case "bench-readme":
		err = runBenchReadme(os.Args[2:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return
	default:
		printUsage(os.Stderr)
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: go run ./cmd/zopfli-task <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  build-upstream       Build the upstream C zopfli binary")
	fmt.Fprintln(w, "  bench-upstream       Run Go vs upstream C benchmarks")
	fmt.Fprintln(w, "  capture-cpu-profile  Capture a benchmark CPU profile and print pprof top")
	fmt.Fprintln(w, "  bench-pgo            Compare baseline and PGO benchmark runs")
	fmt.Fprintln(w, "  bench-summary        Print a markdown benchmark summary table")
	fmt.Fprintln(w, "  bench-readme         Refresh the benchmark section in README.md")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Environment:")
	fmt.Fprintln(w, "  ZOPFLI_UPSTREAM_ROOT  Path to the upstream zopfli checkout")
	fmt.Fprintln(w, "  ZOPFLI_UPSTREAM_EXE   Path to a built upstream zopfli binary")
	fmt.Fprintln(w, "  CC                    C compiler to use when building upstream")
}

func runBuildUpstream(args []string) error {
	fs := flag.NewFlagSet("build-upstream", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	upstreamRoot := fs.String("upstream-root", os.Getenv(upstreamRootEnv), "path to the upstream zopfli checkout")
	outputPath := fs.String("out", "", "path to the built zopfli binary")
	compiler := fs.String("cc", os.Getenv(compilerEnv), "C compiler to use")
	marchNative := fs.Bool("march-native", false, "add -march=native for local machine-specific builds")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	resolvedRoot, err := resolveUpstreamRoot(repoRoot, *upstreamRoot, "")
	if err != nil {
		return err
	}
	output, err := buildUpstream(repoRoot, resolvedRoot, *outputPath, *compiler, *marchNative)
	if err != nil {
		return err
	}

	fmt.Println("Built upstream zopfli at", output)
	fmt.Println("Set", upstreamExeEnv+"="+output)
	return nil
}

func runBenchUpstream(args []string) error {
	fs := flag.NewFlagSet("bench-upstream", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	upstreamRoot := fs.String("upstream-root", os.Getenv(upstreamRootEnv), "path to the upstream zopfli checkout")
	upstreamExe := fs.String("upstream-exe", os.Getenv(upstreamExeEnv), "path to a built upstream zopfli binary")
	benchFilter := fs.String("bench", defaultUpstreamBench, "benchmark filter")
	benchTime := fs.String("benchtime", "", "benchmark time override")
	compiler := fs.String("cc", os.Getenv(compilerEnv), "C compiler to use if upstream needs building")
	marchNative := fs.Bool("march-native", false, "add -march=native when auto-building upstream for local benchmarks")
	autoBuild := fs.Bool("auto-build", true, "build the upstream binary when not found")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	exe, err := resolveUpstreamExe(repoRoot, *upstreamExe, *upstreamRoot, *compiler, *autoBuild, *marchNative)
	if err != nil {
		return err
	}

	goArgs := []string{"test", "-run", "^$", "-bench", *benchFilter, "-benchmem"}
	if normalized := normalizeBenchTime(*benchTime); normalized != "" {
		goArgs = append(goArgs, "-benchtime", normalized)
	}
	goArgs = append(goArgs, "./...")
	_, err = runCommand(repoRoot, map[string]string{upstreamExeEnv: exe}, false, goCommand, goArgs...)
	return err
}

func runCaptureCPUProfile(args []string) error {
	fs := flag.NewFlagSet("capture-cpu-profile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", defaultProfilePath, "path to the CPU profile")
	benchFilter := fs.String("bench", defaultBenchFilter, "benchmark filter")
	benchTime := fs.String("benchtime", defaultBenchTime, "benchmark time")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	normalizedBenchTime := normalizeBenchTime(*benchTime)
	profilePath := filepath.Clean(*profile)
	_, err = runCommand(repoRoot, nil, false, goCommand,
		"test", "-run", "^$", "-bench", *benchFilter, "-benchtime", normalizedBenchTime, "-cpuprofile", profilePath, defaultBenchPackage,
	)
	if err != nil {
		return err
	}
	_, err = runCommand(repoRoot, nil, false, goCommand, "tool", "pprof", "-top", profilePath)
	return err
}

func runBenchPGO(args []string) error {
	fs := flag.NewFlagSet("bench-pgo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", defaultProfilePath, "path to the CPU profile")
	benchFilter := fs.String("bench", defaultBenchFilter, "benchmark filter")
	benchTime := fs.String("benchtime", defaultBenchTime, "benchmark time")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	normalizedBenchTime := normalizeBenchTime(*benchTime)
	profilePath := repoPath(repoRoot, *profile)
	if !fileExists(*profile) {
		if !fileExists(profilePath) {
			return fmt.Errorf("PGO profile not found: %s. Run capture-cpu-profile first", *profile)
		}
	}

	fmt.Println("Baseline:")
	if _, err := runCommand(repoRoot, nil, false, goCommand,
		"test", "-run", "^$", "-bench", *benchFilter, "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(profileDescriptionPrefix + *profile)
	_, err = runCommand(repoRoot, nil, false, goCommand,
		"test", "-pgo", profilePath, "-run", "^$", "-bench", *benchFilter, "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	)
	return err
}

func runBenchSummary(args []string) error {
	summary, err := benchmarkSummary(args)
	if err != nil {
		return err
	}
	fmt.Print(summary)
	return nil
}

func runBenchReadme(args []string) error {
	fs := flag.NewFlagSet("bench-readme", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", defaultProfilePath, "path to the CPU profile")
	benchTime := fs.String("benchtime", defaultBenchTime, "benchmark time")
	upstreamRoot := fs.String("upstream-root", os.Getenv(upstreamRootEnv), "path to the upstream zopfli checkout")
	upstreamExe := fs.String("upstream-exe", os.Getenv(upstreamExeEnv), "path to a built upstream zopfli binary")
	compiler := fs.String("cc", os.Getenv(compilerEnv), "C compiler to use if upstream needs building")
	marchNative := fs.Bool("march-native", false, "add -march=native when auto-building upstream for local benchmarks")
	autoBuild := fs.Bool("auto-build", true, "build the upstream binary when not found")
	readme := fs.String("readme", defaultReadmePath, "path to the README file to update")
	summaryFile := fs.String("summary-file", "", "path to an existing markdown benchmark summary")
	if err := fs.Parse(args); err != nil {
		return usageError(fs, err)
	}

	var (
		summary string
		err     error
	)
	if *summaryFile != "" {
		data, readErr := os.ReadFile(*summaryFile)
		if readErr != nil {
			return fmt.Errorf("read summary file: %w", readErr)
		}
		summary = string(data)
	} else {
		summaryArgs := []string{
			"-profile", *profile,
			"-benchtime", *benchTime,
			"-upstream-root", *upstreamRoot,
			"-upstream-exe", *upstreamExe,
			"-cc", *compiler,
			fmt.Sprintf("-march-native=%t", *marchNative),
			fmt.Sprintf("-auto-build=%t", *autoBuild),
		}
		summary, err = benchmarkSummary(summaryArgs)
		if err != nil {
			return err
		}
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	readmePath := repoPath(repoRoot, *readme)
	return syncReadmeBenchmarkSection(readmePath, summary)
}

func benchmarkSummary(args []string) (string, error) {
	fs := flag.NewFlagSet("bench-summary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.String("profile", defaultProfilePath, "path to the CPU profile")
	benchTime := fs.String("benchtime", defaultBenchTime, "benchmark time")
	upstreamRoot := fs.String("upstream-root", os.Getenv(upstreamRootEnv), "path to the upstream zopfli checkout")
	upstreamExe := fs.String("upstream-exe", os.Getenv(upstreamExeEnv), "path to a built upstream zopfli binary")
	compiler := fs.String("cc", os.Getenv(compilerEnv), "C compiler to use if upstream needs building")
	marchNative := fs.Bool("march-native", false, "add -march=native when auto-building upstream for local benchmarks")
	autoBuild := fs.Bool("auto-build", true, "build the upstream binary when not found")
	if err := fs.Parse(args); err != nil {
		return "", usageError(fs, err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	normalizedBenchTime := normalizeBenchTime(*benchTime)
	profilePath := repoPath(repoRoot, *profile)
	if !fileExists(profilePath) {
		if err := captureCPUProfile(repoRoot, profilePath, defaultBenchFilter, normalizedBenchTime); err != nil {
			return "", err
		}
	}

	exe, err := resolveUpstreamExe(repoRoot, *upstreamExe, *upstreamRoot, *compiler, *autoBuild, *marchNative)
	if err != nil {
		return "", err
	}
	env := map[string]string{upstreamExeEnv: exe}

	rows := map[string]*summaryRow{}

	plainOutput, err := runCommand(repoRoot, nil, true, goCommand,
		"test", "-run", "^$", "-bench", defaultPureGoBench, "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	)
	if err != nil {
		return "", err
	}
	mergeSummary(rows, plainOutput, "go")

	ratioOutput, err := runCommand(repoRoot, env, true, goCommand,
		"test", "-run", "^$", "-bench", defaultCompressionBench, "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	)
	if err != nil {
		return "", err
	}
	mergeSummary(rows, ratioOutput, "ratio")

	pgoOutput, err := runCommand(repoRoot, nil, true, goCommand,
		"test", "-pgo", profilePath, "-run", "^$", "-bench", defaultPureGoBench, "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	)
	if err != nil {
		return "", err
	}
	mergeSummary(rows, pgoOutput, "pgo")

	upstreamOutput, err := runCommand(repoRoot, env, true, goCommand,
		"test", "-run", "^$", "-bench", "UpstreamCGzip", "-benchmem", "-benchtime", normalizedBenchTime, "./...",
	)
	if err != nil {
		return "", err
	}
	mergeSummary(rows, upstreamOutput, "c")

	return renderSummary(rows), nil
}

func captureCPUProfile(repoRoot, profile, benchFilter, benchTime string) error {
	profile = repoPath(repoRoot, profile)
	benchTime = normalizeBenchTime(benchTime)
	_, err := runCommand(repoRoot, nil, false, goCommand,
		"test", "-run", "^$", "-bench", benchFilter, "-benchtime", benchTime, "-cpuprofile", profile, defaultBenchPackage,
	)
	if err != nil {
		return err
	}
	_, err = runCommand(repoRoot, nil, false, goCommand, "tool", "pprof", "-top", profile)
	return err
}

func repoPath(repoRoot, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(repoRoot, filepath.Clean(path))
}

func usageError(fs *flag.FlagSet, cause error) error {
	var b strings.Builder
	b.WriteString(cause.Error())
	b.WriteString("\nusage: ")
	b.WriteString(fs.Name())
	b.WriteString(" ")
	fs.SetOutput(&b)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
	return errors.New(b.String())
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for current := wd; ; current = filepath.Dir(current) {
		goMod := filepath.Join(current, "go.mod")
		if data, readErr := os.ReadFile(goMod); readErr == nil && bytes.Contains(data, []byte(repoModulePath)) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not find repo root from %s", wd)
		}
	}
}

func resolveUpstreamRoot(repoRoot, explicitRoot, explicitExe string) (string, error) {
	for _, candidate := range upstreamRootCandidates(repoRoot, explicitRoot, explicitExe) {
		if isUpstreamRoot(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not locate upstream zopfli checkout; set %s or pass -upstream-root", upstreamRootEnv)
}

func resolveUpstreamExe(repoRoot, explicitExe, explicitRoot, compiler string, autoBuild, marchNative bool) (string, error) {
	for _, candidate := range upstreamExeCandidates(repoRoot, explicitExe, explicitRoot) {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	if !autoBuild {
		return "", fmt.Errorf("could not locate upstream zopfli binary; set %s or run build-upstream", upstreamExeEnv)
	}
	root, err := resolveUpstreamRoot(repoRoot, explicitRoot, explicitExe)
	if err != nil {
		return "", err
	}
	return buildUpstream(repoRoot, root, "", compiler, marchNative)
}

func upstreamRootCandidates(repoRoot, explicitRoot, explicitExe string) []string {
	var candidates []string
	add := func(path string) {
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		for _, existing := range candidates {
			if samePath(existing, cleaned) {
				return
			}
		}
		candidates = append(candidates, cleaned)
	}

	add(explicitRoot)
	add(os.Getenv(upstreamRootEnv))
	for _, derived := range rootsFromExecutable(explicitExe) {
		add(derived)
	}
	for _, derived := range rootsFromExecutable(os.Getenv(upstreamExeEnv)) {
		add(derived)
	}
	add(filepath.Join(repoRoot, "..", "zopfli"))
	add(filepath.Join(repoRoot, "..", "..", "zopfli"))
	add(filepath.Join(repoRoot, "..", "upstream", "zopfli"))
	add(filepath.Join(repoRoot, "zopfli"))

	return candidates
}

func upstreamExeCandidates(repoRoot, explicitExe, explicitRoot string) []string {
	var candidates []string
	add := func(path string) {
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		for _, existing := range candidates {
			if samePath(existing, cleaned) {
				return
			}
		}
		candidates = append(candidates, cleaned)
	}

	add(explicitExe)
	add(os.Getenv(upstreamExeEnv))
	for _, root := range upstreamRootCandidates(repoRoot, explicitRoot, explicitExe) {
		for _, candidate := range executableCandidatesForRoot(root) {
			add(candidate)
		}
	}
	return candidates
}

func rootsFromExecutable(path string) []string {
	if path == "" {
		return nil
	}
	cleaned := filepath.Clean(path)
	dir := filepath.Dir(cleaned)
	parent := filepath.Dir(dir)
	return []string{dir, parent}
}

func executableCandidatesForRoot(root string) []string {
	name := upstreamBinaryBase + exeSuffix()
	var names []string
	if exeSuffix() == ".exe" {
		names = []string{name, upstreamBinaryBase}
	} else {
		names = []string{name, upstreamBinaryBase + ".exe"}
	}

	var candidates []string
	for _, fileName := range names {
		candidates = append(candidates,
			filepath.Join(root, "build-go-bench", fileName),
			filepath.Join(root, fileName),
			filepath.Join(root, "build", fileName),
			filepath.Join(root, "out", fileName),
		)
	}
	return candidates
}

func buildUpstream(repoRoot, upstreamRoot, outputPath, compilerOverride string, marchNative bool) (string, error) {
	if !isUpstreamRoot(upstreamRoot) {
		return "", fmt.Errorf("not an upstream zopfli checkout: %s", upstreamRoot)
	}
	compiler, err := resolveCompiler(repoRoot, compilerOverride)
	if err != nil {
		return "", err
	}
	if outputPath == "" {
		outputPath = filepath.Join(upstreamRoot, "build-go-bench", upstreamBinaryBase+exeSuffix())
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}

	sources, err := filepath.Glob(filepath.Join(upstreamRoot, "src", "zopfli", "*.c"))
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", fmt.Errorf("no upstream C sources found under %s", filepath.Join(upstreamRoot, "src", "zopfli"))
	}
	sort.Strings(sources)

	args := append([]string{}, sources...)
	args = append(args,
		"-O3",
		"-W",
		"-Wall",
		"-Wextra",
		"-Wno-unused-function",
		"-ansi",
		"-pedantic",
	)
	if marchNative {
		args = append(args, "-march=native")
	}
	if runtime.GOOS != "windows" {
		args = append(args, "-lm")
	}
	args = append(args, "-o", outputPath)

	fmt.Fprintf(os.Stderr, "Using compiler %s\n", compiler)
	_, err = runCommand(upstreamRoot, nil, false, compiler, args...)
	if err != nil {
		return "", err
	}
	return filepath.Clean(outputPath), nil
}

func resolveCompiler(repoRoot, override string) (string, error) {
	for _, candidate := range []string{override, os.Getenv(compilerEnv)} {
		if candidate == "" {
			continue
		}
		resolved, err := exec.LookPath(candidate)
		if err == nil {
			return resolved, nil
		}
		if fileExists(candidate) {
			return filepath.Clean(candidate), nil
		}
	}

	repoClang := filepath.Join(repoRoot, "llvm", "bin", "clang"+exeSuffix())
	if fileExists(repoClang) {
		return repoClang, nil
	}
	for _, name := range []string{"clang", "gcc", "cc"} {
		resolved, err := exec.LookPath(name)
		if err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("no usable C compiler found; install clang or gcc, set CC, or use the bundled llvm toolchain")
}

func runCommand(dir string, extraEnv map[string]string, capture bool, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), flattenEnv(extraEnv)...)

	if capture {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("%s %s failed: %w\n%s", name, strings.Join(args, " "), err, string(output))
		}
		return string(output), nil
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return "", nil
}

func flattenEnv(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return env
}

func mergeSummary(rows map[string]*summaryRow, output, mode string) {
	for _, record := range benchmarkRecords(output) {
		switch mode {
		case "go":
			matches := benchPurePattern.FindStringSubmatch(record)
			if len(matches) == 0 {
				continue
			}
			row := summaryFor(rows, matches[1])
			row.GoNs = parseOptional(matches[2])
		case "pgo":
			matches := benchPurePattern.FindStringSubmatch(record)
			if len(matches) == 0 {
				continue
			}
			row := summaryFor(rows, matches[1])
			row.PgoNs = parseOptional(matches[2])
		case "c":
			matches := benchCPattern.FindStringSubmatch(record)
			if len(matches) == 0 {
				continue
			}
			row := summaryFor(rows, matches[1])
			row.CNs = parseOptional(matches[2])
		case "ratio":
			matches := benchRatioPrefix.FindStringSubmatch(record)
			if len(matches) == 0 {
				continue
			}
			row := summaryFor(rows, matches[1])
			parseRatioMetrics(row, matches[4])
		}
	}
}

func benchmarkRecords(output string) []string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	records := make([]string, 0, 32)
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		records = append(records, strings.TrimSpace(current.String()))
		current.Reset()
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "Benchmark") {
			flush()
			current.WriteString(line)
			continue
		}
		if isBenchmarkMetaLine(line) {
			flush()
			continue
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
			current.WriteString(line)
		}
	}
	flush()
	return records
}

func isBenchmarkMetaLine(line string) bool {
	return line == "PASS" || strings.HasPrefix(line, "ok ") || strings.HasPrefix(line, "ok\t") ||
		strings.HasPrefix(line, "goos:") || strings.HasPrefix(line, "goarch:") || strings.HasPrefix(line, "pkg:") ||
		strings.HasPrefix(line, "cpu:") || strings.HasPrefix(line, "?")
}

func summaryFor(rows map[string]*summaryRow, name string) *summaryRow {
	row := rows[name]
	if row == nil {
		row = &summaryRow{Corpus: name}
		rows[name] = row
	}
	return row
}

func parseOptional(value string) optionalFloat {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return optionalFloat{}
	}
	return optionalFloat{value: parsed, ok: true}
}

func parseRatioMetrics(row *summaryRow, metrics string) {
	fields := strings.Fields(metrics)
	for i := 0; i+1 < len(fields); i += 2 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			continue
		}
		switch fields[i+1] {
		case "go_bytes":
			row.GoBytes = optionalFloat{value: value, ok: true}
		case "c_bytes":
			row.CBytes = optionalFloat{value: value, ok: true}
		case "gzip_bytes":
			row.GzipBytes = optionalFloat{value: value, ok: true}
		case "go_ratio":
			row.GoRatio = optionalFloat{value: value, ok: true}
		}
	}
}

func renderSummary(rows map[string]*summaryRow) string {
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	columns := []string{"Corpus", "GoMs", "PgoMs", "CMs", "PGO/Go", "Go/C", "PGO/C", "GoBytes", "CBytes", "GzipBytes"}
	builder.WriteString("| " + strings.Join(columns, " | ") + " |\n")
	builder.WriteString("| " + strings.Join([]string{"---", "---:", "---:", "---:", "---:", "---:", "---:", "---:", "---:", "---:"}, " | ") + " |\n")
	for _, name := range names {
		row := rows[name]
		values := []string{
			row.Corpus,
			formatOptionalMillis(row.GoNs),
			formatOptionalMillis(row.PgoNs),
			formatOptionalMillis(row.CNs),
			formatRatio(row.PgoNs, row.GoNs),
			formatRatio(row.GoNs, row.CNs),
			formatRatio(row.PgoNs, row.CNs),
			formatOptionalWhole(row.GoBytes),
			formatOptionalWhole(row.CBytes),
			formatOptionalWhole(row.GzipBytes),
		}
		builder.WriteString("| " + strings.Join(values, " | ") + " |\n")
	}
	return builder.String()
}

func syncReadmeBenchmarkSection(path, summary string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}
	table, err := extractMarkdownTable(summary)
	if err != nil {
		return err
	}
	updated, err := replaceMarkedSection(string(content), readmeBenchStartMarker, readmeBenchEndMarker,
		table+"\n")
	if err != nil {
		return err
	}
	if updated == string(content) {
		return nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	return nil
}

func extractMarkdownTable(content string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i := 0; i+1 < len(lines); i++ {
		if !isMarkdownTableRow(lines[i]) || !isMarkdownTableSeparator(lines[i+1]) {
			continue
		}

		end := i + 2
		for end < len(lines) && isMarkdownTableRow(lines[end]) {
			end++
		}
		return strings.TrimSpace(strings.Join(lines[i:end], "\n")), nil
	}
	return "", errors.New("benchmark summary does not contain a markdown table")
}

func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

func isMarkdownTableSeparator(line string) bool {
	if !isMarkdownTableRow(line) {
		return false
	}
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(trimmed, "|")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		if cell == "" {
			return false
		}
		for _, r := range cell {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func replaceMarkedSection(content, startMarker, endMarker, body string) (string, error) {
	start := strings.Index(content, startMarker)
	if start == -1 {
		return "", fmt.Errorf("missing README marker %q", startMarker)
	}
	end := strings.Index(content, endMarker)
	if end == -1 {
		return "", fmt.Errorf("missing README marker %q", endMarker)
	}
	if end < start {
		return "", fmt.Errorf("README markers are out of order")
	}
	startBody := start + len(startMarker)
	replacement := content[:startBody] + "\n\n" + strings.TrimSpace(body) + "\n\n" + content[end:]
	return replacement, nil
}

func formatOptionalMillis(value optionalFloat) string {
	if !value.ok {
		return ""
	}
	return formatFloat(value.value/1e6, 2)
}

func formatOptionalWhole(value optionalFloat) string {
	if !value.ok {
		return ""
	}
	return strconv.FormatInt(int64(value.value), 10)
}

func formatRatio(left, right optionalFloat) string {
	if !left.ok || !right.ok || right.value == 0 {
		return ""
	}
	return formatFloat(left.value/right.value, 2)
}

func formatFloat(value float64, precision int) string {
	return strconv.FormatFloat(value, 'f', precision, 64)
}

func normalizeBenchTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := strconv.Atoi(value); err == nil {
		return value + "x"
	}
	return value
}

func isUpstreamRoot(path string) bool {
	return fileExists(filepath.Join(path, filepath.FromSlash(upstreamSourceSentinel)))
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
