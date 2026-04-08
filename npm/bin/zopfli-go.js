#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');
const { getBinaryPath } = require('../lib/runtime');

function main() {
  let binaryPath;
  try {
    binaryPath = getBinaryPath();
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }

  const result = spawnSync(binaryPath, process.argv.slice(2), {
    stdio: 'inherit',
  });

  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }

  process.exit(result.status === null ? 1 : result.status);
}

main();