package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	zopfli "github.com/ralscha/zopfli-go"
)

const gzipExtension = ".gz"

type repeatedValues []string

func (v *repeatedValues) String() string {
	return strings.Join(*v, ",")
}

func (v *repeatedValues) Set(value string) error {
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*v = append(*v, filepath.ToSlash(value))
	return nil
}

type config struct {
	jobs            int
	jsonOutput      bool
	includeSuffixes []string
	excludeSuffixes []string
	options         zopfli.Options
}

type fileCandidate struct {
	sourcePath string
	outputPath string
	matchPath  string
	baseName   string
}

type fileResult struct {
	sourcePath     string
	outputPath     string
	status         string
	originalSize   int
	compressedSize int
	err            error
}

type summaryCounts struct {
	written         int
	skippedBigger   int
	skippedFiltered int
	errors          int
}

type jsonResult struct {
	SourcePath     string `json:"sourcePath"`
	OutputPath     string `json:"outputPath"`
	Status         string `json:"status"`
	OriginalSize   int    `json:"originalSize,omitempty"`
	CompressedSize int    `json:"compressedSize,omitempty"`
	Error          string `json:"error,omitempty"`
}

type jsonSummary struct {
	Written         int `json:"written"`
	SkippedBigger   int `json:"skippedBigger"`
	SkippedFiltered int `json:"skippedFiltered"`
	Errors          int `json:"errors"`
}

type jsonReport struct {
	Summary jsonSummary  `json:"summary"`
	Results []jsonResult `json:"results"`
}

type discoveryResult struct {
	candidates []fileCandidate
	filtered   []fileResult
}

const (
	statusWritten         = "written"
	statusSkippedBigger   = "skipped-bigger"
	statusSkippedFiltered = "skipped-filtered"
	statusError           = "error"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, inputs, showHelp, err := parseArgs(args)
	if showHelp {
		printUsage(stdout)
		return 0
	}
	if err != nil {
		printUsage(stderr)
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	discovery, discoveryErr := discoverCandidates(cfg, inputs)
	if discoveryErr != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", discoveryErr)
		return 1
	}

	processed := processCandidates(cfg, discovery.candidates)
	allResults := append([]fileResult{}, discovery.filtered...)
	allResults = append(allResults, processed...)

	counts, emitErr := emitResults(stdout, stderr, allResults, cfg)
	if emitErr != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", emitErr)
		return 1
	}
	if counts.errors > 0 {
		return 1
	}
	return 0
}

func parseArgs(args []string) (config, []string, bool, error) {
	defaultOptions := zopfli.DefaultOptions()
	cfg := config{
		jobs:    runtime.GOMAXPROCS(0),
		options: defaultOptions,
	}

	var includes repeatedValues
	var excludes repeatedValues

	fs := flag.NewFlagSet("zopfli-go", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&cfg.jobs, "jobs", cfg.jobs, "number of files to compress concurrently")
	fs.IntVar(&cfg.jobs, "j", cfg.jobs, "number of files to compress concurrently")
	fs.Var(&includes, "include-suffix", "repeatable suffix filter for relative paths or base filenames")
	fs.Var(&includes, "i", "repeatable suffix filter for relative paths or base filenames")
	fs.Var(&excludes, "exclude-suffix", "repeatable suffix filter for relative paths or base filenames")
	fs.Var(&excludes, "x", "repeatable suffix filter for relative paths or base filenames")
	fs.IntVar(&cfg.options.NumIterations, "iterations", cfg.options.NumIterations, "number of optimization iterations")
	fs.IntVar(&cfg.options.NumIterations, "n", cfg.options.NumIterations, "number of optimization iterations")
	fs.BoolVar(&cfg.options.BlockSplitting, "block-splitting", cfg.options.BlockSplitting, "enable block splitting")
	fs.BoolVar(&cfg.options.BlockSplittingLast, "block-splitting-last", cfg.options.BlockSplittingLast, "run block splitting after lz77 optimization")
	fs.IntVar(&cfg.options.BlockSplittingMax, "block-splitting-max", cfg.options.BlockSplittingMax, "maximum number of block split points")
	fs.BoolVar(&cfg.options.Verbose, "verbose", cfg.options.Verbose, "print compression progress")
	fs.BoolVar(&cfg.options.Verbose, "v", cfg.options.Verbose, "print compression progress")
	fs.BoolVar(&cfg.options.VerboseMore, "verbose-more", cfg.options.VerboseMore, "print additional compression progress")
	fs.BoolVar(&cfg.options.VerboseMore, "V", cfg.options.VerboseMore, "print additional compression progress")
	fs.BoolVar(&cfg.jsonOutput, "json", cfg.jsonOutput, "print machine-readable JSON output")
	fs.BoolVar(&cfg.jsonOutput, "J", cfg.jsonOutput, "print machine-readable JSON output")
	fs.Bool("h", false, "show help")
	fs.Bool("help", false, "show help")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config{}, nil, true, nil
		}
		return config{}, nil, false, err
	}

	if helpRequested(args) {
		return config{}, nil, true, nil
	}

	cfg.includeSuffixes = append(cfg.includeSuffixes, includes...)
	cfg.excludeSuffixes = append(cfg.excludeSuffixes, excludes...)

	if cfg.jobs <= 0 {
		return config{}, nil, false, fmt.Errorf("-jobs must be greater than 0")
	}
	if cfg.options.NumIterations < 0 {
		return config{}, nil, false, fmt.Errorf("-iterations must be >= 0")
	}
	if cfg.options.BlockSplittingMax < 0 {
		return config{}, nil, false, fmt.Errorf("-block-splitting-max must be >= 0")
	}
	if fs.NArg() == 0 {
		return config{}, nil, false, fmt.Errorf("at least one file or directory path is required")
	}

	return cfg, fs.Args(), false, nil
}

func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "-help" || arg == "--help" {
			return true
		}
	}
	return false
}

func printUsage(w io.Writer) {
	writeLine(w, "Usage: zopfli-go [flags] <file-or-directory> [<file-or-directory>...]")
	writeLine(w, "")
	writeLine(w, "Precompress web assets into adjacent .gz files. Directories are walked recursively.")
	writeLine(w, "Files are skipped when the .gz output would be larger than or equal to the original.")
	writeLine(w, "")
	writeLine(w, "Flags:")
	writeLine(w, "  -j, --jobs int")
	writeLine(w, "    Number of files to compress concurrently (default: logical CPU count)")
	writeLine(w, "  -i, --include-suffix value")
	writeLine(w, "    Repeatable suffix filter for relative paths or base filenames")
	writeLine(w, "  -x, --exclude-suffix value")
	writeLine(w, "    Repeatable suffix filter for relative paths or base filenames")
	writeLine(w, "  -n, --iterations int")
	writeLine(w, "    Number of optimization iterations")
	writeLine(w, "      --block-splitting")
	writeLine(w, "    Enable block splitting (default true)")
	writeLine(w, "      --block-splitting-last")
	writeLine(w, "    Run block splitting after LZ77 optimization")
	writeLine(w, "      --block-splitting-max int")
	writeLine(w, "    Maximum number of block split points")
	writeLine(w, "  -v, --verbose")
	writeLine(w, "    Print per-file write and skip decisions")
	writeLine(w, "  -V, --verbose-more")
	writeLine(w, "    Print additional filter skip information")
	writeLine(w, "  -J, --json")
	writeLine(w, "    Print machine-readable JSON output")
}

func writeLine(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

func discoverCandidates(cfg config, inputs []string) (discoveryResult, error) {
	seen := make(map[string]struct{})
	discovery := discoveryResult{
		candidates: make([]fileCandidate, 0),
		filtered:   make([]fileResult, 0),
	}

	for _, input := range inputs {
		absInput, err := filepath.Abs(input)
		if err != nil {
			return discoveryResult{}, fmt.Errorf("resolve path %q: %w", input, err)
		}

		//nolint:gosec // CLI inputs are explicit user-selected paths for local/CI asset processing.
		info, err := os.Stat(absInput)
		if err != nil {
			return discoveryResult{}, fmt.Errorf("stat %q: %w", input, err)
		}

		if info.IsDir() {
			//nolint:gosec // CLI inputs are explicit user-selected roots to walk recursively.
			err = filepath.WalkDir(absInput, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}

				fileInfo, err := entry.Info()
				if err != nil {
					return err
				}
				if !fileInfo.Mode().IsRegular() {
					return nil
				}

				candidate, ok, filteredResult, err := buildCandidate(absInput, path, cfg)
				if err != nil {
					return err
				}
				if !ok {
					if filteredResult.status != "" {
						discovery.filtered = append(discovery.filtered, filteredResult)
					}
					return nil
				}
				if _, exists := seen[candidate.sourcePath]; exists {
					return nil
				}
				seen[candidate.sourcePath] = struct{}{}
				discovery.candidates = append(discovery.candidates, candidate)
				return nil
			})
			if err != nil {
				return discoveryResult{}, fmt.Errorf("walk %q: %w", input, err)
			}
			continue
		}

		if !info.Mode().IsRegular() {
			return discoveryResult{}, fmt.Errorf("input %q is not a regular file or directory", input)
		}
		candidate, ok, filteredResult, err := buildCandidate(filepath.Dir(absInput), absInput, cfg)
		if err != nil {
			return discoveryResult{}, err
		}
		if !ok {
			if filteredResult.status != "" {
				discovery.filtered = append(discovery.filtered, filteredResult)
			}
			continue
		}
		if _, exists := seen[candidate.sourcePath]; exists {
			continue
		}
		seen[candidate.sourcePath] = struct{}{}
		discovery.candidates = append(discovery.candidates, candidate)
	}

	return discovery, nil
}

func buildCandidate(rootPath, sourcePath string, cfg config) (fileCandidate, bool, fileResult, error) {
	baseName := filepath.Base(sourcePath)
	if strings.EqualFold(filepath.Ext(sourcePath), gzipExtension) {
		return fileCandidate{}, false, fileResult{
			sourcePath: sourcePath,
			outputPath: sourcePath,
			status:     statusSkippedFiltered,
		}, nil
	}

	relPath, err := filepath.Rel(rootPath, sourcePath)
	if err != nil {
		return fileCandidate{}, false, fileResult{}, fmt.Errorf("compute relative path for %q: %w", sourcePath, err)
	}
	relPath = filepath.ToSlash(relPath)
	if relPath == "." {
		relPath = baseName
	}

	if !matchesSuffixFilters(cfg.includeSuffixes, cfg.excludeSuffixes, relPath, baseName) {
		return fileCandidate{}, false, fileResult{
			sourcePath: sourcePath,
			outputPath: sourcePath + gzipExtension,
			status:     statusSkippedFiltered,
		}, nil
	}

	return fileCandidate{
		sourcePath: sourcePath,
		outputPath: sourcePath + gzipExtension,
		matchPath:  relPath,
		baseName:   baseName,
	}, true, fileResult{}, nil
}

func matchesSuffixFilters(includeSuffixes, excludeSuffixes []string, matchPath, baseName string) bool {
	if hasSuffixMatch(excludeSuffixes, matchPath, baseName) {
		return false
	}
	if len(includeSuffixes) == 0 {
		return true
	}
	return hasSuffixMatch(includeSuffixes, matchPath, baseName)
}

func hasSuffixMatch(suffixes []string, matchPath, baseName string) bool {
	for _, suffix := range suffixes {
		normalizedSuffix := filepath.ToSlash(suffix)
		if strings.HasSuffix(matchPath, normalizedSuffix) || strings.HasSuffix(baseName, normalizedSuffix) {
			return true
		}
	}
	return false
}

func processCandidates(cfg config, candidates []fileCandidate) []fileResult {
	if len(candidates) == 0 {
		return nil
	}

	jobs := min(cfg.jobs, len(candidates))

	tasks := make(chan fileCandidate)
	results := make(chan fileResult, len(candidates))

	var wg sync.WaitGroup
	for range jobs {
		wg.Go(func() {
			for candidate := range tasks {
				results <- processCandidate(cfg.options, candidate)
			}
		})
	}

	go func() {
		for _, candidate := range candidates {
			tasks <- candidate
		}
		close(tasks)
		wg.Wait()
		close(results)
	}()

	collected := make([]fileResult, 0, len(candidates))
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func processCandidate(options zopfli.Options, candidate fileCandidate) fileResult {
	data, err := os.ReadFile(candidate.sourcePath)
	if err != nil {
		return fileResult{sourcePath: candidate.sourcePath, outputPath: candidate.outputPath, status: statusError, err: err}
	}

	compressed := zopfli.Compress(&options, zopfli.FormatGzip, data)
	if len(compressed) >= len(data) {
		return fileResult{
			sourcePath:     candidate.sourcePath,
			outputPath:     candidate.outputPath,
			status:         statusSkippedBigger,
			originalSize:   len(data),
			compressedSize: len(compressed),
		}
	}

	//nolint:gosec // Output path is derived from an explicit user-selected input file path.
	if err := os.WriteFile(candidate.outputPath, compressed, 0o600); err != nil {
		return fileResult{sourcePath: candidate.sourcePath, outputPath: candidate.outputPath, status: statusError, err: err}
	}

	return fileResult{
		sourcePath:     candidate.sourcePath,
		outputPath:     candidate.outputPath,
		status:         statusWritten,
		originalSize:   len(data),
		compressedSize: len(compressed),
	}
}

func emitResults(stdout, stderr io.Writer, results []fileResult, cfg config) (summaryCounts, error) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].sourcePath < results[j].sourcePath
	})

	counts := summaryCounts{}
	jsonResults := make([]jsonResult, 0, len(results))
	for _, result := range results {
		switch result.status {
		case statusWritten:
			counts.written++
			if cfg.options.Verbose {
				if _, err := fmt.Fprintf(stdout, "written %s -> %s (%d -> %d bytes)\n", result.sourcePath, result.outputPath, result.originalSize, result.compressedSize); err != nil {
					return summaryCounts{}, err
				}
			}
		case statusSkippedBigger:
			counts.skippedBigger++
			if cfg.options.Verbose {
				if _, err := fmt.Fprintf(stdout, "skipped-bigger %s (%d -> %d bytes)\n", result.sourcePath, result.originalSize, result.compressedSize); err != nil {
					return summaryCounts{}, err
				}
			}
		case statusSkippedFiltered:
			counts.skippedFiltered++
			if cfg.options.VerboseMore {
				if _, err := fmt.Fprintf(stdout, "skipped-filtered %s\n", result.sourcePath); err != nil {
					return summaryCounts{}, err
				}
			}
		case statusError:
			counts.errors++
			if !cfg.jsonOutput {
				if _, err := fmt.Fprintf(stderr, "error: %s: %v\n", result.sourcePath, result.err); err != nil {
					return summaryCounts{}, err
				}
			}
		}

		jsonEntry := jsonResult{
			SourcePath:     result.sourcePath,
			OutputPath:     result.outputPath,
			Status:         result.status,
			OriginalSize:   result.originalSize,
			CompressedSize: result.compressedSize,
		}
		if result.err != nil {
			jsonEntry.Error = result.err.Error()
		}
		jsonResults = append(jsonResults, jsonEntry)
	}

	if cfg.jsonOutput {
		report := jsonReport{
			Summary: jsonSummary{
				Written:         counts.written,
				SkippedBigger:   counts.skippedBigger,
				SkippedFiltered: counts.skippedFiltered,
				Errors:          counts.errors,
			},
			Results: jsonResults,
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			return summaryCounts{}, err
		}
		return counts, nil
	}

	if _, err := fmt.Fprintf(stdout, "Summary: written=%d skipped-bigger=%d skipped-filtered=%d errors=%d\n", counts.written, counts.skippedBigger, counts.skippedFiltered, counts.errors); err != nil {
		return summaryCounts{}, err
	}
	return counts, nil
}
