'use strict';

const {
  buildArgs,
  getBinaryPath,
  normalizeOutput,
  runBinary,
  runBinarySync,
} = require('./lib/runtime');

function precompressSync(inputs, options = {}) {
  return parseReport(runBinarySync(buildArgs(inputs, { ...options, json: true })));
}

function precompress(inputs, options = {}) {
  return runBinary(buildArgs(inputs, { ...options, json: true })).then(parseReport);
}

function parseReport(output) {
  const normalized = normalizeOutput(output);
  if (normalized.length === 0) {
    throw new Error('zopfli-go did not produce JSON output');
  }
  return JSON.parse(normalized);
}

module.exports = {
  buildArgs,
  getBinaryPath,
  precompress,
  precompressSync,
};