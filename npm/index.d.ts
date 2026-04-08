export type InputPaths = string | readonly string[];

export interface PrecompressResultSummary {
  written: number;
  skippedBigger: number;
  skippedFiltered: number;
  errors: number;
}

export interface PrecompressFileResult {
  sourcePath: string;
  outputPath: string;
  status: 'written' | 'skipped-bigger' | 'skipped-filtered' | 'error';
  originalSize?: number;
  compressedSize?: number;
  error?: string;
}

export interface PrecompressResult {
  summary: PrecompressResultSummary;
  results: PrecompressFileResult[];
}

export interface PrecompressOptions {
  jobs?: number;
  includeSuffixes?: string | readonly string[];
  excludeSuffixes?: string | readonly string[];
  iterations?: number;
  blockSplitting?: boolean;
  blockSplittingLast?: boolean;
  blockSplittingMax?: number;
  verbose?: boolean;
  verboseMore?: boolean;
  json?: boolean;
}

export function getBinaryPath(): string;
export function buildArgs(inputs: InputPaths, options?: PrecompressOptions): string[];
export function precompress(inputs: InputPaths, options?: Omit<PrecompressOptions, 'json'>): Promise<PrecompressResult>;
export function precompressSync(inputs: InputPaths, options?: Omit<PrecompressOptions, 'json'>): PrecompressResult;