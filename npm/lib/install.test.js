'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { buildDownloadUrl, renderTemplate } = require('./install');

test('renderTemplate substitutes all placeholders', () => {
  assert.equal(
    renderTemplate('{{binary}}_{{version}}_{{os}}_{{arch}}{{extension}}', {
      arch: 'amd64',
      binary: 'zopfli-go',
      extension: '.exe',
      os: 'windows',
      version: '0.1.0',
    }),
    'zopfli-go_0.1.0_windows_amd64.exe',
  );
});

test('renderTemplate rejects unknown placeholders', () => {
  assert.throws(() => renderTemplate('{{missing}}', {}), /unknown template key/);
});

test('buildDownloadUrl composes the GitHub release asset URL', () => {
  assert.equal(
    buildDownloadUrl({
      version: '0.1.0',
      zopfliGo: {
        owner: 'ralscha',
        repo: 'zopfli-go',
        binaryName: 'zopfli-go',
        tagPrefix: 'v',
        assetNameTemplate: '{{binary}}_{{version}}_{{os}}_{{arch}}{{extension}}',
      },
    }, {
      arch: 'amd64',
      extension: '.exe',
      os: 'windows',
    }),
    'https://github.com/ralscha/zopfli-go/releases/download/v0.1.0/zopfli-go_0.1.0_windows_amd64.exe',
  );
});