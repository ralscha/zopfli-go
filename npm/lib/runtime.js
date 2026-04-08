'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

function getBinaryFileName() {
  return process.platform === 'win32' ? 'zopfli-go.exe' : 'zopfli-go';
}

function getBinaryPath() {
  const binaryPath = path.join(__dirname, '..', 'native', getBinaryFileName());
  if (!fs.existsSync(binaryPath)) {
    throw new Error(
      `zopfli-go binary not found at ${binaryPath}. Run npm install again or set ZOPFLI_GO_DOWNLOAD_URL for postinstall.`,
    );
  }
  return binaryPath;
}

function normalizeInputs(inputs) {
  if (typeof inputs === 'string') {
    if (inputs.length === 0) {
      throw new TypeError('inputs must not be empty');
    }
    return [inputs];
  }

  if (!Array.isArray(inputs) || inputs.length === 0) {
    throw new TypeError('inputs must be a non-empty string or array of strings');
  }

  return inputs.map((input, index) => {
    if (typeof input !== 'string' || input.length === 0) {
      throw new TypeError(`inputs[${index}] must be a non-empty string`);
    }
    return input;
  });
}

function buildArgs(inputs, options = {}) {
  const args = [];

  pushNumberFlag(args, '--jobs', options.jobs, { min: 1 });
  pushRepeatedStringFlag(args, '--include-suffix', options.includeSuffixes);
  pushRepeatedStringFlag(args, '--exclude-suffix', options.excludeSuffixes);
  pushNumberFlag(args, '--iterations', options.iterations, { min: 0 });
  pushBooleanFlag(args, '--block-splitting', options.blockSplitting);
  pushBooleanFlag(args, '--block-splitting-last', options.blockSplittingLast);
  pushNumberFlag(args, '--block-splitting-max', options.blockSplittingMax, { min: 0 });
  pushSimpleFlag(args, '--verbose', options.verbose);
  pushSimpleFlag(args, '--verbose-more', options.verboseMore);
  pushSimpleFlag(args, '--json', options.json);

  args.push(...normalizeInputs(inputs));

  return args;
}

function pushRepeatedStringFlag(args, name, value) {
  if (value === undefined) {
    return;
  }

  const values = typeof value === 'string' ? [value] : value;
  if (!Array.isArray(values) || values.length === 0) {
    throw new TypeError(`${name} must be a string or non-empty array of strings`);
  }

  values.forEach((entry, index) => {
    if (typeof entry !== 'string' || entry.length === 0) {
      throw new TypeError(`${name}[${index}] must be a non-empty string`);
    }
    args.push(name, entry);
  });
}

function pushNumberFlag(args, name, value, { min }) {
  if (value === undefined) {
    return;
  }
  if (!Number.isInteger(value) || value < min) {
    throw new TypeError(`${name} must be an integer >= ${min}`);
  }
  args.push(name, String(value));
}

function pushBooleanFlag(args, name, value) {
  if (value === undefined) {
    return;
  }
  if (typeof value !== 'boolean') {
    throw new TypeError(`${name} must be a boolean`);
  }
  args.push(`${name}=${value ? 'true' : 'false'}`);
}

function pushSimpleFlag(args, name, value) {
  if (value === undefined) {
    return;
  }
  if (typeof value !== 'boolean') {
    throw new TypeError(`${name} must be a boolean`);
  }
  if (value) {
    args.push(name);
  }
}

function runBinarySync(args) {
  const result = spawnSync(getBinaryPath(), args, {
    encoding: 'utf8',
    maxBuffer: 16 * 1024 * 1024,
  });

  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(normalizeOutput(result.stderr) || `zopfli-go exited with code ${result.status}`);
  }

  return normalizeOutput(result.stdout);
}

function runBinary(args) {
  return new Promise((resolve, reject) => {
    const child = spawn(getBinaryPath(), args, {
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    let stdout = '';
    let stderr = '';

    child.stdout.setEncoding('utf8');
    child.stderr.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      stdout += chunk;
    });
    child.stderr.on('data', (chunk) => {
      stderr += chunk;
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code !== 0) {
        reject(new Error(normalizeOutput(stderr) || `zopfli-go exited with code ${code}`));
        return;
      }

      resolve(normalizeOutput(stdout));
    });
  });
}

function normalizeOutput(output) {
  if (!output) {
    return '';
  }
  return String(output).trim();
}

module.exports = {
  buildArgs,
  getBinaryPath,
  normalizeInputs,
  normalizeOutput,
  runBinary,
  runBinarySync,
};