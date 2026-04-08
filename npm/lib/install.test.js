'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const assert = require('node:assert/strict');
const zlib = require('node:zlib');

const { buildDownloadUrl, extractBinaryFromZip, renderTemplate, resolveTarget } = require('./install');

test('renderTemplate substitutes all placeholders', () => {
  assert.equal(
    renderTemplate('{{binary}}_{{version}}_{{os}}_{{arch}}{{archiveExtension}}', {
      arch: 'amd64',
      archiveExtension: '.zip',
      binary: 'zopfli-go',
      extension: '.exe',
      os: 'windows',
      version: '1.0.0',
    }),
    'zopfli-go_1.0.0_windows_amd64.zip',
  );
});

test('renderTemplate rejects unknown placeholders', () => {
  assert.throws(() => renderTemplate('{{missing}}', {}), /unknown template key/);
});

test('buildDownloadUrl composes the GitHub release asset URL', () => {
  assert.equal(
    buildDownloadUrl({
      version: '1.0.0',
      zopfliGo: {
        owner: 'ralscha',
        repo: 'zopfli-go',
        binaryName: 'zopfli-go',
        tagPrefix: 'v',
        assetNameTemplate: '{{binary}}_{{version}}_{{os}}_{{arch}}{{archiveExtension}}',
      },
    }, {
      arch: 'amd64',
      archiveExtension: '.zip',
      extension: '.exe',
      os: 'windows',
    }),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_windows_amd64.zip',
  );
});

test('resolveTarget maps all supported platform and architecture combinations', () => {
  assert.deepEqual(resolveTarget('linux', 'x64'), {
    arch: 'amd64',
    archiveExtension: '',
    extension: '',
    os: 'linux',
  });
  assert.deepEqual(resolveTarget('linux', 'arm64'), {
    arch: 'arm64',
    archiveExtension: '',
    extension: '',
    os: 'linux',
  });
  assert.deepEqual(resolveTarget('darwin', 'x64'), {
    arch: 'amd64',
    archiveExtension: '',
    extension: '',
    os: 'darwin',
  });
  assert.deepEqual(resolveTarget('darwin', 'arm64'), {
    arch: 'arm64',
    archiveExtension: '',
    extension: '',
    os: 'darwin',
  });
  assert.deepEqual(resolveTarget('win32', 'x64'), {
    arch: 'amd64',
    archiveExtension: '.zip',
    extension: '.exe',
    os: 'windows',
  });
  assert.deepEqual(resolveTarget('win32', 'arm64'), {
    arch: 'arm64',
    archiveExtension: '.zip',
    extension: '.exe',
    os: 'windows',
  });
});

test('resolveTarget rejects unsupported platforms and architectures', () => {
  assert.throws(() => resolveTarget('freebsd', 'x64'), /unsupported platform: freebsd/);
  assert.throws(() => resolveTarget('linux', 'arm'), /unsupported architecture: arm/);
  assert.throws(() => resolveTarget('darwin', 'ia32'), /unsupported architecture: ia32/);
});

test('buildDownloadUrl matches GitHub release asset names for all supported targets', () => {
  const packageJson = {
    version: '1.0.0',
    zopfliGo: {
      owner: 'ralscha',
      repo: 'zopfli-go',
      binaryName: 'zopfli-go',
      tagPrefix: 'v',
      assetNameTemplate: '{{binary}}_{{version}}_{{os}}_{{arch}}{{archiveExtension}}',
    },
  };

  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('linux', 'x64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_linux_amd64',
  );
  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('linux', 'arm64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_linux_arm64',
  );
  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('darwin', 'x64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_darwin_amd64',
  );
  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('darwin', 'arm64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_darwin_arm64',
  );
  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('win32', 'x64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_windows_amd64.zip',
  );
  assert.equal(
    buildDownloadUrl(packageJson, resolveTarget('win32', 'arm64')),
    'https://github.com/ralscha/zopfli-go/releases/download/v1.0.0/zopfli-go_1.0.0_windows_arm64.zip',
  );
});

test('extractBinaryFromZip extracts the expected binary from a Windows archive', () => {
  const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), 'zopfli-go-'));
  const zipPath = path.join(temporaryDirectory, 'archive.zip');
  const destination = path.join(temporaryDirectory, 'zopfli-go.exe');

  fs.writeFileSync(zipPath, createZipArchive([
    { name: 'README.md', data: Buffer.from('readme'), method: 0 },
    { name: 'zopfli-go.exe', data: Buffer.from('binary-data'), method: 8 },
  ]));

  extractBinaryFromZip(zipPath, 'zopfli-go.exe', destination);

  assert.equal(fs.readFileSync(destination, 'utf8'), 'binary-data');
  fs.rmSync(temporaryDirectory, { force: true, recursive: true });
});

function createZipArchive(entries) {
  const localRecords = [];
  const centralRecords = [];
  let localOffset = 0;

  for (const entry of entries) {
    const fileName = Buffer.from(entry.name, 'utf8');
    const compressedData = entry.method === 8 ? zlib.deflateRawSync(entry.data) : entry.data;
    const crc32 = zlib.crc32(entry.data);

    const localHeader = Buffer.alloc(30);
    localHeader.writeUInt32LE(0x04034b50, 0);
    localHeader.writeUInt16LE(20, 4);
    localHeader.writeUInt16LE(0, 6);
    localHeader.writeUInt16LE(entry.method, 8);
    localHeader.writeUInt32LE(0, 10);
    localHeader.writeUInt32LE(crc32, 14);
    localHeader.writeUInt32LE(compressedData.length, 18);
    localHeader.writeUInt32LE(entry.data.length, 22);
    localHeader.writeUInt16LE(fileName.length, 26);
    localHeader.writeUInt16LE(0, 28);

    const centralHeader = Buffer.alloc(46);
    centralHeader.writeUInt32LE(0x02014b50, 0);
    centralHeader.writeUInt16LE(20, 4);
    centralHeader.writeUInt16LE(20, 6);
    centralHeader.writeUInt16LE(0, 8);
    centralHeader.writeUInt16LE(entry.method, 10);
    centralHeader.writeUInt32LE(0, 12);
    centralHeader.writeUInt32LE(crc32, 16);
    centralHeader.writeUInt32LE(compressedData.length, 20);
    centralHeader.writeUInt32LE(entry.data.length, 24);
    centralHeader.writeUInt16LE(fileName.length, 28);
    centralHeader.writeUInt16LE(0, 30);
    centralHeader.writeUInt16LE(0, 32);
    centralHeader.writeUInt16LE(0, 34);
    centralHeader.writeUInt16LE(0, 36);
    centralHeader.writeUInt32LE(0, 38);
    centralHeader.writeUInt32LE(localOffset, 42);

    localRecords.push(localHeader, fileName, compressedData);
    centralRecords.push(centralHeader, fileName);
    localOffset += localHeader.length + fileName.length + compressedData.length;
  }

  const centralDirectory = Buffer.concat(centralRecords);
  const endOfCentralDirectory = Buffer.alloc(22);
  endOfCentralDirectory.writeUInt32LE(0x06054b50, 0);
  endOfCentralDirectory.writeUInt16LE(0, 4);
  endOfCentralDirectory.writeUInt16LE(0, 6);
  endOfCentralDirectory.writeUInt16LE(entries.length, 8);
  endOfCentralDirectory.writeUInt16LE(entries.length, 10);
  endOfCentralDirectory.writeUInt32LE(centralDirectory.length, 12);
  endOfCentralDirectory.writeUInt32LE(localOffset, 16);
  endOfCentralDirectory.writeUInt16LE(0, 20);

  return Buffer.concat([...localRecords, centralDirectory, endOfCentralDirectory]);
}