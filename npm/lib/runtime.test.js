'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { buildArgs, normalizeInputs } = require('./runtime');

test('buildArgs appends input paths after flags', () => {
  assert.deepEqual(buildArgs('public'), ['public']);
});

test('buildArgs maps all supported options', () => {
  assert.deepEqual(buildArgs(['public', 'assets/app.js'], {
    jobs: 4,
    includeSuffixes: ['.js', '.css'],
    excludeSuffixes: '.min.js',
    iterations: 3,
    blockSplitting: false,
    blockSplittingLast: true,
    blockSplittingMax: 9,
    verbose: true,
    verboseMore: false,
    json: true,
  }), [
    '--jobs',
    '4',
    '--include-suffix',
    '.js',
    '--include-suffix',
    '.css',
    '--exclude-suffix',
    '.min.js',
    '--iterations',
    '3',
    '--block-splitting=false',
    '--block-splitting-last=true',
    '--block-splitting-max',
    '9',
    '--verbose',
    '--json',
    'public',
    'assets/app.js',
  ]);
});

test('buildArgs rejects unsupported values', () => {
  assert.throws(() => buildArgs([], {}), /inputs must be a non-empty string or array of strings/);
  assert.throws(() => buildArgs('public', { jobs: 0 }), /--jobs/);
  assert.throws(() => buildArgs('public', { iterations: -1 }), /--iterations/);
  assert.throws(() => buildArgs('public', { blockSplitting: 'yes' }), /--block-splitting/);
  assert.throws(() => buildArgs('public', { includeSuffixes: ['.js', ''] }), /--include-suffix\[1\]/);
});

test('normalizeInputs accepts string and arrays of strings', () => {
  assert.deepEqual(normalizeInputs('public'), ['public']);
  assert.deepEqual(normalizeInputs(['public', 'assets/app.js']), ['public', 'assets/app.js']);
});

test('normalizeInputs rejects unsupported input values', () => {
  assert.throws(() => normalizeInputs(123), /inputs must be a non-empty string or array of strings/);
  assert.throws(() => normalizeInputs(['public', '']), /inputs\[1\] must be a non-empty string/);
});