'use strict';

const fs = require('node:fs');
const path = require('node:path');
const https = require('node:https');
const zlib = require('node:zlib');

const TAR_BLOCK_SIZE = 512;

const ROOT_DIR = path.resolve(__dirname, '..', '..');
const PACKAGE_JSON_PATH = path.join(ROOT_DIR, 'package.json');
const NATIVE_DIR = path.join(ROOT_DIR, 'npm', 'native');

async function installBinary() {
  if (process.env.ZOPFLI_GO_SKIP_DOWNLOAD === '1') {
    return;
  }

  const packageJson = JSON.parse(fs.readFileSync(PACKAGE_JSON_PATH, 'utf8'));
  const target = resolveTarget();
  const binaryFileName = `${packageJson.zopfliGo.binaryName}${target.extension}`;
  const destination = path.join(NATIVE_DIR, binaryFileName);

  fs.mkdirSync(NATIVE_DIR, { recursive: true });

  const url = process.env.ZOPFLI_GO_DOWNLOAD_URL || buildDownloadUrl(packageJson, target);
  const temporaryFile = `${destination}${target.archiveExtension}.download`;
  await download(url, temporaryFile);

  if (target.archiveType === 'zip') {
    extractBinaryFromZip(temporaryFile, binaryFileName, destination);
    fs.rmSync(temporaryFile, { force: true });
  } else if (target.archiveType === 'tar.gz') {
    extractBinaryFromTarGz(temporaryFile, binaryFileName, destination);
    fs.rmSync(temporaryFile, { force: true });
  } else {
    fs.renameSync(temporaryFile, destination);
  }

  if (process.platform !== 'win32') {
    fs.chmodSync(destination, 0o755);
  }
}

function resolveTarget(platform = process.platform, architecture = process.arch) {
  const platformMap = {
    darwin: 'darwin',
    linux: 'linux',
    win32: 'windows',
  };
  const archMap = {
    arm64: 'arm64',
    x64: 'amd64',
  };

  const os = platformMap[platform];
  if (!os) {
    throw new Error(`unsupported platform: ${platform}`);
  }

  const arch = archMap[architecture];
  if (!arch) {
    throw new Error(`unsupported architecture: ${architecture}`);
  }

  return {
    arch,
    archiveExtension: platform === 'win32' ? '.zip' : '.tar.gz',
    archiveType: platform === 'win32' ? 'zip' : 'tar.gz',
    extension: platform === 'win32' ? '.exe' : '',
    os,
  };
}

function buildDownloadUrl(packageJson, target) {
  const config = packageJson.zopfliGo;
  const version = packageJson.version;
  const tag = `${config.tagPrefix || ''}${version}`;
  const assetName = renderTemplate(config.assetNameTemplate, {
    arch: target.arch,
    archiveExtension: target.archiveExtension,
    binary: config.binaryName,
    extension: target.extension,
    os: target.os,
    version,
  });

  return `https://github.com/${config.owner}/${config.repo}/releases/download/${tag}/${assetName}`;
}

function renderTemplate(template, values) {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key) => {
    if (!(key in values)) {
      throw new Error(`unknown template key: ${key}`);
    }
    return values[key];
  });
}

function extractBinaryFromZip(zipPath, binaryFileName, destination) {
  const archive = fs.readFileSync(zipPath);
  const entry = findZipEntry(archive, binaryFileName);

  if (!entry) {
    throw new Error(`binary ${binaryFileName} not found in archive ${zipPath}`);
  }

  const dataOffset = entry.localHeaderOffset + 30 + entry.localFileNameLength + entry.localExtraLength;
  const compressedData = archive.subarray(dataOffset, dataOffset + entry.compressedSize);

  let data;
  switch (entry.compressionMethod) {
    case 0:
      data = compressedData;
      break;
    case 8:
      data = zlib.inflateRawSync(compressedData, {
        maxOutputLength: entry.uncompressedSize,
      });
      break;
    default:
      throw new Error(`unsupported zip compression method: ${entry.compressionMethod}`);
  }

  if (data.length !== entry.uncompressedSize) {
    throw new Error(`zip entry size mismatch for ${binaryFileName}`);
  }

  fs.writeFileSync(destination, data);
}

function extractBinaryFromTarGz(tarGzPath, binaryFileName, destination) {
  const archive = zlib.gunzipSync(fs.readFileSync(tarGzPath));
  const entry = findTarEntry(archive, binaryFileName);

  if (!entry) {
    throw new Error(`binary ${binaryFileName} not found in archive ${tarGzPath}`);
  }

  fs.writeFileSync(destination, entry.data);
}

function findTarEntry(archive, binaryFileName) {
  for (let offset = 0; offset + TAR_BLOCK_SIZE <= archive.length;) {
    const header = archive.subarray(offset, offset + TAR_BLOCK_SIZE);
    if (isZeroBlock(header)) {
      return null;
    }

    const name = readTarString(header, 0, 100);
    const prefix = readTarString(header, 345, 155);
    const size = readTarOctal(header, 124, 12);
    const typeflag = readTarString(header, 156, 1) || '0';
    const fullName = prefix ? `${prefix}/${name}` : name;
    const fileName = fullName.split('/').pop();
    const dataOffset = offset + TAR_BLOCK_SIZE;
    const dataEnd = dataOffset + size;

    if (dataEnd > archive.length) {
      throw new Error('invalid tar archive: entry exceeds archive size');
    }

    if ((typeflag === '0' || typeflag === '') && fileName === binaryFileName) {
      return {
        data: archive.subarray(dataOffset, dataEnd),
      };
    }

    offset = dataOffset + alignTarSize(size);
  }

  throw new Error('invalid tar archive: missing end-of-archive marker');
}

function alignTarSize(size) {
  return Math.ceil(size / TAR_BLOCK_SIZE) * TAR_BLOCK_SIZE;
}

function isZeroBlock(block) {
  for (const byte of block) {
    if (byte !== 0) {
      return false;
    }
  }
  return true;
}

function readTarString(buffer, offset, length) {
  const value = buffer.subarray(offset, offset + length).toString('utf8');
  const terminatorIndex = value.indexOf('\0');
  return (terminatorIndex === -1 ? value : value.slice(0, terminatorIndex)).trim();
}

function readTarOctal(buffer, offset, length) {
  const raw = readTarString(buffer, offset, length).trim();
  if (!raw) {
    return 0;
  }

  if (!/^[0-7]+$/.test(raw)) {
    throw new Error(`invalid tar size field: ${raw}`);
  }

  return Number.parseInt(raw, 8);
}

function findZipEntry(archive, binaryFileName) {
  const endOfCentralDirectory = findEndOfCentralDirectory(archive);
  let offset = endOfCentralDirectory.centralDirectoryOffset;
  const limit = offset + endOfCentralDirectory.centralDirectorySize;

  while (offset < limit) {
    if (archive.readUInt32LE(offset) !== 0x02014b50) {
      throw new Error('invalid zip central directory header');
    }

    const compressionMethod = archive.readUInt16LE(offset + 10);
    const crc32 = archive.readUInt32LE(offset + 16);
    const compressedSize = archive.readUInt32LE(offset + 20);
    const uncompressedSize = archive.readUInt32LE(offset + 24);
    const fileNameLength = archive.readUInt16LE(offset + 28);
    const extraLength = archive.readUInt16LE(offset + 30);
    const commentLength = archive.readUInt16LE(offset + 32);
    const localHeaderOffset = archive.readUInt32LE(offset + 42);
    const entryName = archive.subarray(offset + 46, offset + 46 + fileNameLength).toString('utf8');
    const fileName = entryName.split(/[\\/]/).pop();

    if (fileName === binaryFileName) {
      if (archive.readUInt32LE(localHeaderOffset) !== 0x04034b50) {
        throw new Error('invalid zip local file header');
      }

      return {
        compressionMethod,
        crc32,
        compressedSize,
        localExtraLength: archive.readUInt16LE(localHeaderOffset + 28),
        localFileNameLength: archive.readUInt16LE(localHeaderOffset + 26),
        localHeaderOffset,
        uncompressedSize,
      };
    }

    offset += 46 + fileNameLength + extraLength + commentLength;
  }

  return null;
}

function findEndOfCentralDirectory(archive) {
  const minimumOffset = Math.max(0, archive.length - 0xffff - 22);

  for (let offset = archive.length - 22; offset >= minimumOffset; offset -= 1) {
    if (archive.readUInt32LE(offset) !== 0x06054b50) {
      continue;
    }

    return {
      centralDirectoryOffset: archive.readUInt32LE(offset + 16),
      centralDirectorySize: archive.readUInt32LE(offset + 12),
    };
  }

  throw new Error('invalid zip end of central directory');
}

function download(url, destination) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(destination);

    const fail = (error) => {
      file.destroy();
      fs.rmSync(destination, { force: true });
      reject(error);
    };

    const request = https.get(url, (response) => {
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        file.close(() => {
          fs.rmSync(destination, { force: true });
          download(response.headers.location, destination).then(resolve, reject);
        });
        return;
      }

      if (response.statusCode !== 200) {
        response.resume();
        fail(new Error(`download failed: ${response.statusCode} ${response.statusMessage || ''} (${url})`));
        return;
      }

      response.pipe(file);
      file.on('finish', () => {
        file.close((closeError) => {
          if (closeError) {
            fail(closeError);
            return;
          }
          resolve();
        });
      });
    });

    request.on('error', fail);
    file.on('error', fail);
  });
}

module.exports = {
  buildDownloadUrl,
  extractBinaryFromTarGz,
  extractBinaryFromZip,
  findEndOfCentralDirectory,
  findTarEntry,
  findZipEntry,
  installBinary,
  renderTemplate,
  resolveTarget,
};