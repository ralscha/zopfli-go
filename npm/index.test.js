'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const runtimePath = require.resolve('./lib/runtime');
const indexPath = require.resolve('./index');

test('precompressSync forces json output and parses the report', () => {
  const result = withStubbedRuntime({
    buildArgs(inputs, options) {
      assert.deepEqual(inputs, ['public']);
      assert.equal(options.jobs, 4);
      assert.equal(options.json, true);
      return ['--json', 'public'];
    },
    runBinarySync(args) {
      assert.deepEqual(args, ['--json', 'public']);
      return '{"summary":{"written":1,"skippedBigger":0,"skippedFiltered":0,"errors":0},"results":[{"sourcePath":"public/app.js","outputPath":"public/app.js.gz","status":"written","originalSize":100,"compressedSize":42}]}';
    },
  }, (indexModule) => indexModule.precompressSync(['public'], { jobs: 4 }));

  assert.equal(result.summary.written, 1);
  assert.equal(result.results[0].status, 'written');
  assert.equal(result.results[0].compressedSize, 42);
});

test('precompress parses async json output', async () => {
  const result = await withStubbedRuntime({
    buildArgs(inputs, options) {
      assert.equal(inputs, 'public');
      assert.deepEqual(options.includeSuffixes, ['.js']);
      assert.equal(options.json, true);
      return ['--json', '--include-suffix', '.js', 'public'];
    },
    runBinary(args) {
      assert.deepEqual(args, ['--json', '--include-suffix', '.js', 'public']);
      return Promise.resolve('{"summary":{"written":0,"skippedBigger":1,"skippedFiltered":2,"errors":0},"results":[{"sourcePath":"public/app.min.js","outputPath":"public/app.min.js.gz","status":"skipped-bigger","originalSize":10,"compressedSize":12}]}');
    },
  }, (indexModule) => indexModule.precompress('public', { includeSuffixes: ['.js'] }));

  assert.equal(result.summary.skippedBigger, 1);
  assert.equal(result.summary.skippedFiltered, 2);
  assert.equal(result.results[0].status, 'skipped-bigger');
});

function withStubbedRuntime(stubs, callback) {
  delete require.cache[indexPath];
  delete require.cache[runtimePath];

  const runtime = require('./lib/runtime');
  const originalEntries = new Map();
  for (const [key, stub] of Object.entries(stubs)) {
    originalEntries.set(key, runtime[key]);
    runtime[key] = stub;
  }

  delete require.cache[indexPath];
  const indexModule = require('./index');

  let callbackResult;
  try {
    callbackResult = callback(indexModule);
  } catch (error) {
    restoreRuntime(runtime, originalEntries);
    delete require.cache[indexPath];
    delete require.cache[runtimePath];
    throw error;
  }

  if (callbackResult && typeof callbackResult.then === 'function') {
    return callbackResult.finally(() => {
      restoreRuntime(runtime, originalEntries);
      delete require.cache[indexPath];
      delete require.cache[runtimePath];
    });
  }

  restoreRuntime(runtime, originalEntries);
  delete require.cache[indexPath];
  delete require.cache[runtimePath];
  return callbackResult;
}

function restoreRuntime(runtime, originalEntries) {
  for (const [key, value] of originalEntries.entries()) {
    runtime[key] = value;
  }
}